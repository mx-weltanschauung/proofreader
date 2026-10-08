package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"proofreader/internal/limit"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/stats"
)

const (
	searchDefaultLimit = 50
	searchMaxLimit     = 100
	// searchSlots — сколько поисков считается одновременно. Тот же приём и то
	// же число, что у тяжёлых рендеров краулеру (internal/seo/handler.go,
	// heavySlots), и по той же причине: на боевом два ядра и 3.8 ГБ без
	// свопа, а пул pgx на 25 соединений общий со всеми остальными запросами
	// читальни. Конфиг ru намеренно без стоп-слов, поэтому «и» — запрос по
	// почти всему корпусу (49 тыс. полос из 50 810, 1.5 с на этой машине);
	// без потолка десяток таких разом кладёт и чтение, и выдачу.
	searchSlots = 2
	// searchQueueWait — сколько запрос ждёт свободный слот, прежде чем
	// получить отказ. В отличие от краулера (тот ждать не может: отвалится по
	// таймауту, а работа продолжится вхолостую) живой читатель ждёт ответа
	// сам, и очередь на пару секунд ему полезнее немедленного отказа: обычный
	// поиск укладывается в доли секунды, так что всплеск из трёх-четырёх
	// читателей рассасывается внутри ожидания и никто отказа не видит.
	searchQueueWait = 2 * time.Second
	// searchRetryAfterSeconds — что сказать в Retry-After при отказе. Слот
	// освобождается за секунды, а не за минуты.
	searchRetryAfterSeconds = 5
)

// SearchHandler — публичный полнотекстовый поиск: каталог и тома одним
// запросом, полосы тома — вторым. Спека:
// docs/superpowers/specs/2026-09-03-full-text-search-design.md
type SearchHandler struct {
	store SearchStore
	// slots — ограничитель одновременных поисков; общий с MCP-сервером:
	// см. Slots. Заводится на обработчик, а не на процесс: обработчик в
	// приложении один (cmd/server/main.go), а тесту нужен свой.
	slots limit.Slots
	// wait — сколько ждать слот. Поле, а не константа в теле: тесту нужно
	// ожидание в миллисекундах, иначе проверка отказа стоила бы две секунды.
	wait time.Duration
	// rec — посещаемость: текст запроса и число найденного. nil — не пишем.
	rec        EventRecorder
	trustProxy bool
}

// NewSearchHandler creates a new search handler.
func NewSearchHandler(store SearchStore) *SearchHandler {
	return &SearchHandler{
		store: store,
		slots: limit.New(searchSlots),
		wait:  searchQueueWait,
	}
}

// Slots — ограничитель поиска. Отдаётся MCP-серверу читальни (internal/mcp):
// его поиски встают в те же два слота, что и поиски сайта, но сперва проходят
// свои ворота ёмкостью 1 — второй слот всегда остаётся читателю сайта.
func (h *SearchHandler) Slots() limit.Slots { return h.slots }

// WithRecorder подключает статистику поиска. Текст запроса пишет обработчик,
// а не маячок SPA: только здесь известно, что запрос ничего не нашёл.
func (h *SearchHandler) WithRecorder(rec EventRecorder, trustProxy bool) *SearchHandler {
	h.rec, h.trustProxy = rec, trustProxy
	return h
}

// errSearchBusy — ограничитель занят и ожидание вышло. Отдельный признак, а
// не ошибка репозитория: отвечать на него надо тем же 503 и той же формой
// {"message": …}, что и на исчерпанный statement_timeout, но другими словами
// — уточнять запрос тут бесполезно.
var errSearchBusy = errors.New("search is busy")

// underSlot выполняет тяжёлый запрос под слотом ограничителя. Слот держится
// ровно на время запроса и не захватывает запись ответа: медленный читатель
// иначе занимал бы его, пока качает выдачу.
func (h *SearchHandler) underSlot(r *http.Request, f func() error) error {
	release, ok := h.slots.Acquire(r.Context(), h.wait)
	if !ok {
		return errSearchBusy
	}
	// defer, а не вызов после f: net/http ловит панику обработчика сам и
	// закрывает только соединение — процесс живёт дальше, а неосвобождённый
	// слот утекает навсегда, и после двух таких паник поиск мёртв до
	// перезапуска.
	defer release()
	return f()
}

// optionalInt64 разбирает необязательный числовой параметр. Отсутствие — nil
// и ok; мусор — !ok.
func optionalInt64(r *http.Request, name string) (*int64, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, false
	}
	return &n, true
}

// searchRefusal — отказ поиска, у которого есть свои слова: исчерпанный
// statement_timeout и занятые слоты. Общий для сайта и каталога OPDS
// (opds_source.go), чтобы читатель в читалке видел те же слова, что на
// сайте. retryAfter — секунды для Retry-After, 0 — заголовка нет.
func searchRefusal(err error) (status int, msg string, retryAfter int, ok bool) {
	switch {
	case errors.Is(err, repository.ErrSearchTimeout):
		return http.StatusServiceUnavailable, "Поиск занял слишком долго, уточните запрос", 0, true
	case errors.Is(err, errSearchBusy):
		return http.StatusServiceUnavailable,
			"Поиск сейчас занят, повторите через несколько секунд", searchRetryAfterSeconds, true
	}
	return 0, "", 0, false
}

func writeSearchError(w http.ResponseWriter, err error) {
	status, msg, retry, ok := searchRefusal(err)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Поиск не удался")
		return
	}
	if retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
	}
	writeError(w, status, msg)
}

// Search — GET /api/search?q=…[&edition_id=…][&terms_only=1].
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := models.NormalizeSearchText(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "Пустой запрос")
		return
	}
	// До ветки terms_only: подсветка в режиме чтения ходит тем же маршрутом,
	// и гонять для неё разбор односимвольного запроса незачем.
	if models.SearchTextTooShort(q) {
		writeError(w, http.StatusBadRequest, models.SearchTooShortMessage)
		return
	}
	editions, ok := parseIDList(r, "editions")
	if !ok {
		writeError(w, http.StatusBadRequest, "editions должен быть списком чисел через запятую")
		return
	}
	// edition_id — прежнее имя параметра; он публичный, на него ссылаются
	// чужие закладки и письма. Читается как область из одного собрания.
	if legacy, ok := optionalInt64(r, "edition_id"); !ok {
		writeError(w, http.StatusBadRequest, "edition_id должен быть числом")
		return
	} else if legacy != nil {
		editions = append(editions, *legacy)
	}
	works, ok := parseIDList(r, "works")
	if !ok {
		writeError(w, http.StatusBadRequest, "works должен быть списком чисел через запятую")
		return
	}
	query := models.SearchQuery{Text: q, EditionIDs: editions, WorkIDs: works}
	ctx := r.Context()

	// Режим чтения по прямой ссылке с ?q= просит только леммы для подсветки;
	// каталог и тома ему не нужны, и гонять их зря не надо.
	if r.URL.Query().Get("terms_only") == "1" {
		terms, err := h.store.Terms(ctx, query)
		if err != nil {
			writeSearchError(w, err)
			return
		}
		if len(terms) == 0 {
			writeError(w, http.StatusBadRequest, "Пустой запрос")
			return
		}
		writeJSONStatus(w, http.StatusOK, models.SearchTerms{Query: q, Terms: terms})
		return
	}

	// Ограничитель стоит только вокруг тяжёлого запроса. Ветка terms_only
	// выше слот не занимает: это один websearch_to_tsquery без похода в
	// таблицы, а стоит она перед подсветкой в режиме чтения — отказывать ей
	// из-за чужого поиска по всему корпусу незачем.
	var res *models.SearchResult
	err := h.underSlot(r, func() error {
		var err error
		res, err = h.store.Search(ctx, query)
		return err
	})
	if err != nil {
		writeSearchError(w, err)
		return
	}
	if len(res.Terms) == 0 {
		writeError(w, http.StatusBadRequest, "Пустой запрос")
		return
	}
	// Пустой срез из репозитория приходит nil, а json пишет его как null;
	// клиент зовёт по этим спискам .map сразу.
	if res.Chapters == nil {
		res.Chapters = []models.SearchChapter{}
	}
	if res.Concepts == nil {
		res.Concepts = []models.SearchConcept{}
	}
	if res.Volumes == nil {
		res.Volumes = []models.SearchVolume{}
	}
	for i := range res.Volumes {
		if res.Volumes[i].Pages == nil {
			res.Volumes[i].Pages = []models.SearchPage{}
		}
	}
	// Поиск лежит на публичном подроутере: сотрудники и боты не должны
	// попадать в «Что ищут» (как и в маячок).
	if h.rec != nil && !isStaffRequest(r) && stats.BotFamily(r.UserAgent()) == "" {
		hits := res.FoundCount()
		h.rec.Record(stats.Event{
			Row: stats.Row{TS: time.Now(), Channel: stats.ChannelSearch, Kind: "search",
				Query: stats.CleanQuery(q), Hits: &hits},
			IP: clientIP(r, h.trustProxy), UA: r.UserAgent(),
		})
	}
	writeJSONStatus(w, http.StatusOK, res)
}

// Pages — GET /api/search/pages?q=…&work_id=…[&limit=50][&offset=0].
func (h *SearchHandler) Pages(w http.ResponseWriter, r *http.Request) {
	q := models.NormalizeSearchText(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "Пустой запрос")
		return
	}
	if models.SearchTextTooShort(q) {
		writeError(w, http.StatusBadRequest, models.SearchTooShortMessage)
		return
	}
	workID, ok := optionalInt64(r, "work_id")
	if !ok || workID == nil {
		writeError(w, http.StatusBadRequest, "work_id обязателен и должен быть числом")
		return
	}
	limit := searchDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "limit должен быть положительным числом")
			return
		}
		limit = n
	}
	if limit > searchMaxLimit {
		limit = searchMaxLimit
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset должен быть неотрицательным числом")
			return
		}
		offset = n
	}
	chapters, ok := parseIDList(r, "chapters")
	if !ok {
		writeError(w, http.StatusBadRequest, "chapters должен быть списком чисел через запятую")
		return
	}

	var res *models.SearchPagesResult
	err := h.underSlot(r, func() error {
		var err error
		res, err = h.store.SearchPages(r.Context(), models.SearchQuery{Text: q}, *workID, chapters, limit, offset)
		return err
	})
	if err != nil {
		writeSearchError(w, err)
		return
	}
	if len(res.Terms) == 0 {
		writeError(w, http.StatusBadRequest, "Пустой запрос")
		return
	}
	if res.Pages == nil {
		res.Pages = []models.SearchPage{}
	}
	if res.Chapters == nil {
		res.Chapters = []models.SearchChapterFacet{}
	}
	writeJSONStatus(w, http.StatusOK, res)
}
