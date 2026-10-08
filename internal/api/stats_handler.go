package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"proofreader/internal/metrics"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/opds"
	"proofreader/internal/repository"
	"proofreader/internal/stats"
)

// hitsPerMinute — предел маячков с одного адреса. Человек, листающий главы,
// до него не дотянется; поток мусора в базу — не пройдёт.
const hitsPerMinute = 120

// hitsGlobalPerMinute — потолок принятых маячков со всех адресов разом:
// предел на адрес не держит того, у кого адресов много, а свёртка хранится
// вечно.
const hitsGlobalPerMinute = 6000

// maxHitBody — маячок несёт путь и источник; больше — мусор.
const maxHitBody = 4 << 10

// EventRecorder — *stats.Recorder. Интерфейс у потребителя: тесты подменяют.
type EventRecorder interface {
	Record(stats.Event)
}

// StatsReader — чтение посещаемости для /admin/stats (*repository.StatsRepository).
type StatsReader interface {
	Traffic(ctx context.Context, from, today time.Time) ([]repository.TrafficRow, error)
	Top(ctx context.Context, channel, kind string, from, today time.Time, limit int) ([]repository.TopRow, error)
	Referrers(ctx context.Context, from, today time.Time, limit int) ([]repository.CountRow, error)
	Searches(ctx context.Context, from, today time.Time, limit int) (*repository.SearchStats, error)
	Crawlers(ctx context.Context, from, today time.Time) ([]repository.CrawlerRow, error)
	Devices(ctx context.Context, from, today time.Time) ([]repository.DeviceRow, error)
}

// HealthSource — *metrics.Metrics.
type HealthSource interface {
	Health() (*metrics.Health, error)
}

// StatsHandler — маячок SPA, серверные каналы и экран /admin/stats.
type StatsHandler struct {
	rec        EventRecorder
	reader     StatsReader
	health     HealthSource
	trustProxy bool
	limiter    *stats.RateLimiter
	global     *stats.RateLimiter
	now        func() time.Time
}

func NewStatsHandler(rec EventRecorder, reader StatsReader, health HealthSource, trustProxy bool) *StatsHandler {
	return &StatsHandler{rec: rec, reader: reader, health: health, trustProxy: trustProxy,
		limiter: stats.NewRateLimiter(hitsPerMinute), global: stats.NewRateLimiter(hitsGlobalPerMinute), now: time.Now}
}

type hitBody struct {
	Path   string `json:"path"`
	Ref    string `json:"ref"`
	Device string `json:"device"`
}

// Hit — POST /api/hit. Отвечает 204 на всё: причину отказа (бот, мусор,
// предел) незачем сообщать тому, кто шлёт мусор.
func (h *StatsHandler) Hit(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(http.StatusNoContent)
	if h == nil || h.rec == nil {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxHitBody+1))
	if err != nil || len(raw) > maxHitBody {
		return
	}
	var body hitBody
	if json.Unmarshal(raw, &body) != nil {
		return
	}
	ua := r.UserAgent()
	if stats.BotFamily(ua) != "" {
		return // Googlebot исполняет JS и шлёт маячки
	}
	if isStaffRequest(r) {
		return // проверки сотрудников забили бы топ
	}
	target, ok := stats.ParsePath(body.Path)
	if !ok {
		return
	}
	ip := clientIP(r, h.trustProxy)
	if !h.limiter.Allow(limiterKey(ip)) || !h.global.Allow("all") {
		return
	}
	h.rec.Record(stats.Event{
		Row: stats.Row{TS: h.now(), Channel: stats.ChannelSPA, Kind: target.Kind,
			WorkID: target.WorkID, EntityID: target.EntityID, SlugKey: target.SlugKey,
			RefHost: stats.RefHost(body.Ref, r.Host), Device: stats.DeviceClass(body.Device)},
		IP: ip, UA: ua,
	})
}

// isStaffRequest — запрос вошедшего редактора или администратора: их
// проверки и поиски не должны попадать в статистику читателей.
func isStaffRequest(r *http.Request) bool {
	claims, ok := middleware.GetUserFromContext(r.Context())
	return ok && (claims.Role == models.RoleEditor || claims.Role == models.RoleAdministrator)
}

// limiterKey — ключ предела частоты: IPv6 по /64 (абонент свободно меняет
// младшие 64 бита), IPv4 как есть. Тот же приём, что у hashIP.
func limiterKey(ip string) string {
	if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
		return parsed.Mask(net.CIDRMask(64, 128)).String()
	}
	return ip
}

// staticArchiveDownloadPath — скачивание архива статической читальни: оно
// отвечает 302 на файл в бакете, а не 200 с телом, и считается по переходу.
const staticArchiveDownloadPath = "/api/static-archive/download"

// Channels — middleware серверных каналов: /seo (HTML краулеру), .md и
// llms.txt (через /seo, см. frontend/nginx.conf), скачивания. Пишется только
// 200 (и 302 скачивания архива): 304 — повтор, 404/410/503 — не отдача.
func (h *StatsHandler) Channels(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := middleware.NewStatusWriter(w)
		next.ServeHTTP(sw, r)
		if h == nil || h.rec == nil || r.Method != http.MethodGet {
			return
		}
		served := sw.Status == http.StatusOK ||
			(sw.Status == http.StatusFound && r.URL.Path == staticArchiveDownloadPath)
		if !served {
			return
		}
		row, ok := channelRow(r)
		if !ok {
			return
		}
		ua := r.UserAgent()
		row.TS = h.now()
		row.Agent = stats.BotFamily(ua)
		h.rec.Record(stats.Event{Row: row, IP: clientIP(r, h.trustProxy), UA: ua})
	})
}

// channelRow — канал и сущность серверного запроса; false — не канал.
func channelRow(r *http.Request) (stats.Row, bool) {
	p := r.URL.Path
	if rest, ok := strings.CutPrefix(p, "/seo"); ok {
		if rest == "/llms.txt" {
			return stats.Row{Channel: stats.ChannelMD, Kind: "llms"}, true
		}
		channel := stats.ChannelSEO
		if md, ok := strings.CutSuffix(rest, ".md"); ok {
			channel = stats.ChannelMD
			rest = md
			// …/chapters/{id}/part-N — часть той же главы
			if i := strings.LastIndex(rest, "/part-"); i >= 0 {
				rest = rest[:i]
			}
		}
		if rest == "" {
			rest = "/"
		}
		t, ok := stats.ParsePath(rest)
		if !ok {
			return stats.Row{}, false
		}
		return stats.Row{Channel: channel, Kind: t.Kind, WorkID: t.WorkID, EntityID: t.EntityID, SlugKey: t.SlugKey}, true
	}
	// Ленты каталога OPDS. Поиск каталога пишет себя сам (channel search,
	// agent opds, см. OPDSSource.Search) и здесь не считается дважды.
	if route, ok := opds.ParsePath(p); ok && route.Kind != opds.KindSearch {
		entity := route.EditionID
		if route.ChapterID != 0 {
			entity = route.ChapterID
		}
		return stats.Row{Channel: stats.ChannelOPDS, Kind: route.Kind, WorkID: route.WorkID, EntityID: entity}, true
	}
	if p == staticArchiveDownloadPath {
		return stats.Row{Channel: stats.ChannelDownload, Kind: "archive"}, true
	}
	if rest, ok := strings.CutSuffix(p, "/download"); ok && strings.HasPrefix(rest, "/api/") {
		t, ok := stats.ParsePath(strings.TrimPrefix(rest, "/api"))
		if !ok || (t.Kind != "work" && t.Kind != "chapter" && t.Kind != "collection") {
			return stats.Row{}, false
		}
		return stats.Row{Channel: stats.ChannelDownload, Kind: t.Kind, WorkID: t.WorkID,
			EntityID: t.EntityID, SlugKey: r.URL.Query().Get("format")}, true
	}
	return stats.Row{}, false
}

const (
	defaultStatsDays = 30
	maxStatsDays     = 365
	defaultTopLimit  = 50
	maxTopLimit      = 500
)

// statsTopKinds — что можно спросить у топа.
var statsTopKinds = map[string]bool{"work": true, "chapter": true, "page": true, "read": true,
	"edition": true, "document": true, "collection": true, "concept": true}

var statsChannels = map[string]bool{stats.ChannelSPA: true, stats.ChannelMD: true,
	stats.ChannelDownload: true, stats.ChannelSEO: true}

// period — [from, today] по ?days= (1…365, по умолчанию 30); today — сегодня по МСК.
func (h *StatsHandler) period(r *http.Request) (from, today time.Time, ok bool) {
	days := defaultStatsDays
	if s := r.URL.Query().Get("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxStatsDays {
			return time.Time{}, time.Time{}, false
		}
		days = n
	}
	today = stats.Day(h.now())
	return today.AddDate(0, 0, -(days - 1)), today, true
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (h *StatsHandler) Traffic(w http.ResponseWriter, r *http.Request) {
	from, today, ok := h.period(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "days — число от 1 до 365")
		return
	}
	rows, err := h.reader.Traffic(r.Context(), from, today)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать посещаемость")
		return
	}
	writeJSONStatus(w, http.StatusOK, nonNil(rows))
}

func (h *StatsHandler) Top(w http.ResponseWriter, r *http.Request) {
	from, today, ok := h.period(r)
	q := r.URL.Query()
	kind, channel := q.Get("kind"), q.Get("channel")
	if channel == "" {
		channel = stats.ChannelSPA
	}
	limit := defaultTopLimit
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxTopLimit {
			ok = false
		}
		limit = n
	}
	if !ok || !statsTopKinds[kind] || !statsChannels[channel] {
		writeError(w, http.StatusBadRequest, "Неверные kind, channel, days или limit")
		return
	}
	rows, err := h.reader.Top(r.Context(), channel, kind, from, today, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать топ")
		return
	}
	writeJSONStatus(w, http.StatusOK, nonNil(rows))
}

func (h *StatsHandler) Referrers(w http.ResponseWriter, r *http.Request) {
	from, today, ok := h.period(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "days — число от 1 до 365")
		return
	}
	rows, err := h.reader.Referrers(r.Context(), from, today, defaultTopLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать источники")
		return
	}
	writeJSONStatus(w, http.StatusOK, nonNil(rows))
}

func (h *StatsHandler) Searches(w http.ResponseWriter, r *http.Request) {
	from, today, ok := h.period(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "days — число от 1 до 365")
		return
	}
	s, err := h.reader.Searches(r.Context(), from, today, defaultTopLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать поисковые запросы")
		return
	}
	s.Frequent, s.Zero = nonNil(s.Frequent), nonNil(s.Zero)
	writeJSONStatus(w, http.StatusOK, s)
}

func (h *StatsHandler) Crawlers(w http.ResponseWriter, r *http.Request) {
	from, today, ok := h.period(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "days — число от 1 до 365")
		return
	}
	rows, err := h.reader.Crawlers(r.Context(), from, today)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать обход краулеров")
		return
	}
	writeJSONStatus(w, http.StatusOK, nonNil(rows))
}

func (h *StatsHandler) Devices(w http.ResponseWriter, r *http.Request) {
	from, today, ok := h.period(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "days — число от 1 до 365")
		return
	}
	rows, err := h.reader.Devices(r.Context(), from, today)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать устройства")
		return
	}
	writeJSONStatus(w, http.StatusOK, nonNil(rows))
}

func (h *StatsHandler) Health(w http.ResponseWriter, r *http.Request) {
	if h.health == nil {
		writeError(w, http.StatusServiceUnavailable, "Метрики не подключены")
		return
	}
	hl, err := h.health.Health()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось собрать метрики")
		return
	}
	hl.Routes = nonNil(hl.Routes)
	writeJSONStatus(w, http.StatusOK, hl)
}
