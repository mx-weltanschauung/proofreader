package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// StaticArchive — текущая сборка статической читальни, как её показывает
// справка (/help#offline). Служебное из манифеста (состав, md5, коммит,
// предыдущая сборка) наружу не идёт.
type StaticArchive struct {
	File   string `json:"file"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Date   string `json:"date"`
	Works  int    `json:"works"`
}

const (
	// staticArchiveTTL — сколько держится прочитанный манифест. Выкладка
	// сборки бывает раз в недели, а справку открывают часто.
	staticArchiveTTL = 5 * time.Minute
	// staticArchiveRetryTTL — сколько держится отказ: лежащий бакет не должен
	// получать запрос с каждого открытия справки.
	staticArchiveRetryTTL = time.Minute
	maxStaticManifest     = 1 << 20
)

var (
	errStaticArchiveAbsent = errors.New("архив не выложен")
	staticArchiveFile      = regexp.MustCompile(`^chitalnya-(\d{4}-\d{2}-\d{2})\.zip$`)
	staticArchiveSHA       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// StaticArchiveHandler — GET /api/static-archive и /api/static-archive/download.
// Сведения об архиве берутся из manifest.json бакета анонимным GET: бакет
// открыт на чтение объектов, ключи бэкенду не нужны. Спека:
// docs/superpowers/specs/2026-10-07-static-reading-room-publish-design.md.
type StaticArchiveHandler struct {
	base   string // STATIC_ARCHIVE_URL без завершающего «/»; пусто — архива нет
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	archive *StaticArchive
	err     error
	until   time.Time
}

func NewStaticArchiveHandler(base string, client *http.Client) *StaticArchiveHandler {
	return &StaticArchiveHandler{base: strings.TrimRight(base, "/"), client: client, now: time.Now}
}

// Get — сведения о текущей сборке для справки.
func (h *StaticArchiveHandler) Get(w http.ResponseWriter, r *http.Request) {
	a, err := h.current(r.Context())
	if err != nil {
		staticArchiveFail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSONStatus(w, http.StatusOK, a)
}

// Download — переход на файл текущей сборки. Через бэкенд, а не прямой
// ссылкой, ради счёта скачиваний: канал download, вид archive
// (StatsHandler.Channels).
func (h *StaticArchiveHandler) Download(w http.ResponseWriter, r *http.Request) {
	a, err := h.current(r.Context())
	if err != nil {
		staticArchiveFail(w, err)
		return
	}
	http.Redirect(w, r, a.URL, http.StatusFound)
}

func staticArchiveFail(w http.ResponseWriter, err error) {
	if errors.Is(err, errStaticArchiveAbsent) {
		writeError(w, http.StatusNotFound, "Архив читальни сейчас не выложен")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "Сведения об архиве читальни сейчас недоступны")
}

// current — сборка из манифеста, с кэшем. Замок держится и на время запроса к
// бакету: одновременные открытия справки после истечения кэша ждут один
// запрос, а не шлют каждый свой.
func (h *StaticArchiveHandler) current(ctx context.Context) (*StaticArchive, error) {
	if h.base == "" {
		return nil, errStaticArchiveAbsent
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.now().Before(h.until) {
		return h.archive, h.err
	}
	// Без отмены от запроса читателя: ушедший читатель иначе оставил бы в кэше
	// на минуту отказ, которого не было.
	a, err := h.fetch(context.WithoutCancel(ctx))
	ttl := staticArchiveTTL
	if err != nil {
		ttl = staticArchiveRetryTTL
		if !errors.Is(err, errStaticArchiveAbsent) {
			log.Printf("static-archive: %v", err)
		}
	}
	h.archive, h.err, h.until = a, err, h.now().Add(ttl)
	return a, err
}

func (h *StaticArchiveHandler) fetch(ctx context.Context) (*StaticArchive, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+"/manifest.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("манифест архива: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden:
		// Бакет открыт на чтение объектов без листинга, и отсутствующий ключ
		// S3 вправе отвечать 403, а не 404.
		return nil, errStaticArchiveAbsent
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("манифест архива: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxStaticManifest+1))
	if err != nil {
		return nil, fmt.Errorf("манифест архива: %w", err)
	}
	if len(body) > maxStaticManifest {
		return nil, errors.New("манифест архива больше 1 МБ")
	}
	return parseStaticManifest(body, h.base)
}

// parseStaticManifest разбирает manifest.json (пишет static-publish.sh). Адрес
// файла собирается из base и имени, а не берётся из манифеста: подменённый
// манифест не уведёт кнопку «Скачать» на чужой хост.
func parseStaticManifest(body []byte, base string) (*StaticArchive, error) {
	var m struct {
		Format  int `json:"format"`
		Current *struct {
			File   string `json:"file"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
			Date   string `json:"date"`
			Works  int    `json:"works"`
		} `json:"current"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("манифест архива не разобран: %w", err)
	}
	if m.Format != 1 {
		return nil, fmt.Errorf("манифест архива: формат %d, ждали 1", m.Format)
	}
	c := m.Current
	if c == nil {
		return nil, errStaticArchiveAbsent
	}
	name := staticArchiveFile.FindStringSubmatch(c.File)
	switch {
	case name == nil:
		return nil, fmt.Errorf("манифест архива: имя файла %q", c.File)
	case name[1] != c.Date:
		return nil, fmt.Errorf("манифест архива: дата %q не та, что в имени %q", c.Date, c.File)
	case !staticArchiveSHA.MatchString(c.SHA256):
		return nil, fmt.Errorf("манифест архива: sha256 %q", c.SHA256)
	case c.Size <= 0 || c.Works <= 0:
		return nil, fmt.Errorf("манифест архива: размер %d, работ %d", c.Size, c.Works)
	}
	return &StaticArchive{File: c.File, URL: base + "/" + c.File, Size: c.Size,
		SHA256: c.SHA256, Date: c.Date, Works: c.Works}, nil
}
