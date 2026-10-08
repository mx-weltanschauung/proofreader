package seo

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"proofreader/internal/site"
	"strconv"
	"strings"
	"sync"
	"time"

	"proofreader/internal/limit"
)

const (
	// heavySlots — сколько глав рендерится одновременно. Рендер главы стоит
	// столько же, сколько её выгрузка, а на боевом два ядра: третий
	// одновременный обход отобрал бы процессор у живых читателей.
	heavySlots = 2
	// retryAfterSeconds — что сказать краулеру при отказе. Гуглобот
	// воспринимает 503 с Retry-After как «сбавь темп», а не как поломку.
	retryAfterSeconds = 30
	// cacheBytes — потолок кэша готовых страниц. Глава на 300 полос — около
	// мегабайта, так что тридцати двух хватает на десятки глав, а память
	// боевого (3.8 ГБ на всё) не страдает.
	cacheBytes = 32 << 20
	// textPartsBudget — сколько байт частей текста один рендер кладёт в кэш.
	// Самое большое понятие указателя — 69 частей по ~160 КБ, около 10 МБ:
	// без предела одно чтение чат-ботом вытесняло бы треть кэша, а
	// вытесненные главы заново рендерились бы под heavySlots. Обычные главы и
	// понятия укладываются целиком, и «один рендер на все части» для них
	// держится; длинный текст кэшируется окном вокруг запрошенной части.
	textPartsBudget = cacheBytes / 8
	// cacheTTL — сколько живёт готовая страница. Кэш нужен от всплеска обхода, а
	// не от повторного захода через неделю: машинная вычитка правит полосы
	// каждый день, и страница без срока годности отдавала бы вчерашний текст,
	// пока её не вытеснит объёмом.
	cacheTTL = time.Hour
	// listMaxAge — сколько браузеру и краулеру держать списочные страницы, у
	// которых нет собственной метки правки (главная, списки).
	listMaxAge = 600
	// cardCacheBytes — потолок кэша карточек. PNG 1200×630 весит 30—60 КБ,
	// так что восьми мегабайт хватает на полторы сотни штук.
	cardCacheBytes = 8 << 20
	// cardCacheTTL — свой, длинный срок годности карточек: Телеграм всё равно
	// забирает картинку один раз и держит у себя сам, а вытеснять карточки
	// обходом текста незачем.
	cardCacheTTL = 24 * time.Hour
	// cacheEntryOverhead — фиксированная надбавка к весу записи сверх ключа и
	// тела/перенаправления: узел container/list, сам cacheRecord, запись
	// карты (заголовок строки-ключа плюс указатель). Прикидка, не точный
	// подсчёт байт — назначение только одно: у записи-перенаправления
	// (cacheEntry.redirect, тело пустое) вес не должен оказаться нулевым,
	// иначе она никогда не подпадёт под вытеснение по c.size > c.max (что и
	// было до этой правки — см. TestDocCachePutEvictsOldRedirectEntries).
	cacheEntryOverhead = 128
)

// Handler отдаёт страницы краулерам. Таблица маршрутов повторяет маршруты SPA
// (frontend/src/App.tsx), а не API: менять её будут вместе с фронтом, и
// держать её рядом с рендерерами честнее, чем в router.go.
type Handler struct {
	src   *Source
	cache *docCache
	// cards — отдельный кэш карточек og:image, не общий со страницами: у
	// карточек свой, куда более долгий срок годности (см. cardCacheTTL).
	cards *docCache
	heavy limit.Slots

	flightMu sync.Mutex
	flights  map[string]*inflight
}

// inflight — рендер, который уже кто-то делает. Второй запрос по тому же
// адресу не начинает свой, а ждёт чужого результата: разосланная ссылка на
// свежую главу приходит от Телеграма, ВК и Слака почти одновременно, и без
// этого оба слота ограничителя уходят на одну и ту же работу, а остальным
// достаётся 503 ровно в момент всплеска, ради которого ограничитель и
// заведён.
type inflight struct {
	done  chan struct{}
	entry *cacheEntry
	err   error
}

// HeavySlots — тяжёлые слоты рендера, для метрик занятости.
func (h *Handler) HeavySlots() limit.Slots { return h.heavy }

func NewHandler(src *Source) *Handler {
	return &Handler{
		src:     src,
		cache:   newDocCache(cacheBytes, cacheTTL),
		cards:   newDocCache(cardCacheBytes, cardCacheTTL),
		heavy:   limit.New(heavySlots),
		flights: map[string]*inflight{},
	}
}

// Page — вход краулера. nginx приводит сюда запрос краулера как
// /seo<путь читальни> (см. frontend/nginx.conf). Второй вход — Text, для
// MCP-сервера; путь к готовой записи у них общий (resolve).
func (h *Handler) Page(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/seo")
	if path == "" {
		path = "/"
	}
	// Подрубрика понятия — единственный параметр, который меняет ответ: он
	// уходит в путь канонической формой (rubricQuery) и тем становится частью
	// ключа кэша и склейки. Прочие параметры, как и раньше, не значат ничего.
	if raw := r.URL.Query().Get("rubric_path"); raw != "" && strings.HasSuffix(path, ".md") {
		rubric := ParseRubricPath(raw)
		if rubric == nil {
			http.Error(w, "Подрубрика в адресе не разбирается", http.StatusNotFound)
			return
		}
		path += rubricQuery(rubric)
	}
	entry, err := h.resolve(r.Context(), path, tryHeavy)
	switch {
	case err == nil:
		writeCached(w, r, entry)
	case errors.Is(err, ErrUnknownPath):
		http.NotFound(w, r)
	case r.Context().Err() != nil:
		// Краулер ушёл — держать соединение незачем. Но поломка ведущего
		// (рендер идёт под WithoutCancel, его ошибка — не обрыв) в журнал
		// попасть обязана: иначе уход краулера прятал бы её молча.
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("seo %s: %v", path, err)
		}
	default:
		writeRenderError(w, r, path, err)
	}
}

// resolve — путь к готовой записи: кэш, склейка одновременных запросов, канон,
// тяжёлый слот, рендер. Общий у Page (краулер) и Text (MCP-сервер). Внутри
// полёта слота не ждёт никто: acquire зовётся с wait=false, а MCP ждёт слот
// снаружи, до resolve (см. Text).
func (h *Handler) resolve(ctx context.Context, path string, acquire AcquireFunc) (*cacheEntry, error) {
	if cached, ok := h.cache.get(path); ok {
		return cached, nil
	}

	// Метка поколения снимается ДО всякой работы. Если том снимут, пока идёт
	// рендер, сброс кэша сдвинет поколение, и готовая страница снятого тома
	// в кэш уже не ляжет (putIfFresh ниже). Иначе сброс отменялся бы сам
	// собой, и снятый том жил бы в кэше свой час.
	gen := h.cache.generation()

	route, ok := match(path)
	if !ok {
		return nil, ErrUnknownPath
	}

	// Ведущим становится первый: он рендерит, остальные ждут его результата.
	// Без этого разосланная ссылка на одну главу занимает оба слота одной и
	// той же работой, и живым краулерам достаётся 503.
	h.flightMu.Lock()
	if fl, ok := h.flights[path]; ok {
		h.flightMu.Unlock()
		select {
		case <-fl.done:
		case <-ctx.Done():
			// Краулер ушёл — держать соединение незачем.
			return nil, ctx.Err()
		}
		// fl.entry, а не fl.err: если ведущий запаниковал, оба поля остались
		// nil (defer снимает с карты и закрывает done и при панике), и
		// проверка по err пропустила бы ждущего к writeCached с нулевым
		// entry — паника вместо внятного 500.
		if fl.entry == nil {
			if fl.err == nil {
				return nil, errors.New("рендер прерван")
			}
			return nil, fl.err
		}
		return fl.entry, nil
	}
	fl := &inflight{done: make(chan struct{})}
	h.flights[path] = fl
	h.flightMu.Unlock()

	defer func() {
		h.flightMu.Lock()
		delete(h.flights, path)
		h.flightMu.Unlock()
		// Закрывать после снятия с карты и после того, как fl.entry/fl.err
		// проставлены: ждущие читают их сразу за done.
		close(fl.done)
	}()

	fl.entry, fl.err = h.lead(ctx, path, route, gen, acquire)
	return fl.entry, fl.err
}

// lead — работа ведущего: канон лёгким запросом до слота, затем слот и рендер.
func (h *Handler) lead(ctx context.Context, path string, route route, gen uint64, acquire AcquireFunc) (*cacheEntry, error) {
	// Сверка канона идёт ДО слота: адрес без слага — это весь нынешний индекс
	// поисковика (80 томов и 11 775 глав), и рендер главы ради выброшенного
	// тела занял бы оба тяжёлых слота (heavySlots = 2) на боевом с двумя
	// ядрами. Стоит после регистрации полёта намеренно: одновременные заходы
	// по одному старому адресу склеиваются, и лёгкий запрос делает один из
	// них, а не каждый сам по себе.
	if route.canonical != nil {
		want, err := route.canonical(context.WithoutCancel(ctx), h.src)
		if err != nil {
			return nil, err
		}
		if want != path {
			entry := &cacheEntry{redirect: want}
			h.cache.putIfFresh(path, entry, gen)
			return entry, nil
		}
	}

	// Тяжёлый рендер идёт только со свободным слотом. Внутри полёта его не
	// ждёт никто (wait=false): MCP-сервер ждёт снаружи, до resolve (см. Text).
	if route.heavy {
		release, err := acquire(ctx, h.heavy, false)
		if err != nil {
			// Любой отказ захвата — занятость, а не поломка: его увидят и
			// ждущие этого полёта, у которых свои правила ответа. Краулер на
			// чужую ошибку занятости (или на уход ведущего) обязан получить
			// 503 с Retry-After, а не 500.
			if errors.Is(err, ErrBusy) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", ErrBusy, err)
		}
		defer release()
	}

	// Контекст ведущего запроса перестаёт быть общим ровно потому, что
	// результат общий: если первый краулер отвалится по таймауту (у
	// Телеграма окно короткое, а тяжёлые рендеры — это как раз главы),
	// контекст ведущего (ctx) отменится, render вернёт context.Canceled, оно
	// ляжет в fl.err — и все ждущие получат 500 из-за чужого обрыва, хотя
	// рендер сам по себе был бы успешным. WithoutCancel сохраняет значения
	// контекста, но снимает отмену и дедлайн — ведущий дорендерит начатое
	// независимо от того, кто его запросил. Ждущие по-прежнему выходят по
	// собственному <-ctx.Done() в resolve: это их соединение, не общий рендер.
	var entry *cacheEntry
	switch {
	case route.textParts != nil:
		chapter, docs, err := route.textParts(context.WithoutCancel(ctx), h.src)
		if errors.Is(err, ErrNoSuchRubric) {
			// Отказ ложится в кэш, как часть за пределами главы: иначе
			// перебор выдуманных подрубрик большого понятия занимал бы
			// тяжёлый слот и сборку адресов на каждый заход.
			entry = &cacheEntry{notFound: true, notFoundText: noSuchRubricText}
			h.cache.putIfFresh(path, entry, gen)
			return entry, nil
		}
		if err != nil {
			return nil, err
		}
		// Все части — в кэш под их каноническими адресами, с тем же gen:
		// одна часть стоит рендера всей главы, и сборщик, читающий главу
		// подряд, иначе платил бы полным рендером и слотом за каждую. Метка
		// та же, что у запрошенной части, поэтому сброс при снятии тома
		// отменяет и эти записи. Путь запроса здесь уже канонический (301
		// отработал выше), так что запрошенная часть — одна из положенных.
		//
		// Не больше textPartsBudget байт за рендер: запрошенная часть — всегда
		// (она и есть ответ), затем следующие по порядку (читатель идёт
		// вперёд), затем предыдущие, от ближайшей.
		used := 0
		put := func(k int, force bool) bool {
			td := docs[k]
			if !force && used+len(td.Body) > textPartsBudget {
				return false
			}
			used += len(td.Body)
			p := chapterTextPath(chapter, k+1) + route.query
			e := textEntry(p, h.build(), td)
			h.cache.putIfFresh(p, e, gen)
			if k+1 == route.part {
				entry = e
			}
			return true
		}
		at := route.part - 1
		if at < len(docs) {
			put(at, true)
		} else {
			at = len(docs)
		}
		for k := at + 1; k < len(docs) && put(k, false); k++ {
		}
		for k := at - 1; k >= 0 && put(k, false); k-- {
		}
		if entry == nil {
			// Номер за пределами главы. Отказ тоже ложится в кэш: иначе
			// /part-99.md длинной главы стоил бы полного рендера на каждый
			// заход.
			entry = &cacheEntry{notFound: true}
			h.cache.putIfFresh(path, entry, gen)
		}
	case route.text != nil:
		td, err := route.text(context.WithoutCancel(ctx), h.src)
		if err != nil {
			return nil, err
		}
		entry = textEntry(path, h.build(), td)
		h.cache.putIfFresh(path, entry, gen)
	default:
		doc, err := route.render(context.WithoutCancel(ctx), h.src)
		if err != nil {
			return nil, err
		}
		entry = &cacheEntry{body: Render(doc)}
		if !doc.CacheKey.IsZero() {
			entry.etag = docETag(path, h.build(), doc.CacheKey.Unix())
		}
		h.cache.putIfFresh(path, entry, gen)
	}
	return entry, nil
}

// TextResult — текст для нейросети, готовый к отдаче MCP-серверу.
type TextResult struct {
	// Path — канонический путь текста без домена: .md главы, части, тома или
	// /llms.txt.
	Path string
	Body string
	// Canonical — абсолютный адрес HTML-страницы того же материала; пусто у
	// /llms.txt.
	Canonical string
}

// maxTextResolves — сколько раз Text проходит resolve: запрошенный путь и не
// больше двух переходов. Построитель ведёт сразу на канон, но в кэше живёт
// (до часа) и запись голый номер -> прежний слаг: смена слага даёт два шага —
// устаревшее перенаправление и новое. Третий переход означал бы петлю.
const maxTextResolves = 3

// Text — текст по пути .md или /llms.txt тем же путём, что у Page: кэш,
// склейка, канон, тяжёлый слот, — поэтому снятие тома (Purge) отменяет и его.
// Неканонический путь проходится до канона. acquire решает, ждать ли тяжёлого
// слота, когда его зовут с wait=true.
//
// Ждёт Text только СНАРУЖИ полёта. .md главы nginx отдаёт через /seo любому
// клиенту, поэтому MCP-сервер и краулер по одному адресу попадают в один
// полёт (h.flights). Жди ведущий слота внутри полёта, краулер, пришедший
// следом, ждал бы вместе с ним десятки секунд — а краулеру ждать слота
// нельзя. Поэтому сперва resolve без ожидания; занято — слот берётся с
// ожиданием вне всякого полёта, и уже с ним resolve повторяется. Ждущие любого
// полёта ждут, таким образом, только рендера, у которого слот уже есть.
func (h *Handler) Text(ctx context.Context, path string, acquire AcquireFunc) (*TextResult, error) {
	// Строка запроса у текста бывает одна — подрубрика понятия в канонической
	// форме (RubricQuery); её разбирает match, здесь — только вид пути.
	if bare, _, _ := strings.Cut(path, "?"); bare != "/llms.txt" && !strings.HasSuffix(bare, ".md") {
		return nil, ErrUnknownPath
	}
	noWait := func(ctx context.Context, heavy limit.Slots, _ bool) (func(), error) {
		return acquire(ctx, heavy, false)
	}
	for i := 0; i < maxTextResolves; i++ {
		entry, err := h.resolve(ctx, path, noWait)
		if errors.Is(err, ErrBusy) {
			rel, aerr := acquire(ctx, h.heavy, true)
			if aerr != nil {
				// Уход клиента остаётся ошибкой контекста: занятостью его
				// считать нельзя, по ней меряют отказы.
				if errors.Is(aerr, ErrBusy) || ctx.Err() != nil {
					return nil, aerr
				}
				return nil, fmt.Errorf("%w: %v", ErrBusy, aerr)
			}
			// Слот уже в руках: ведущий получит его вместо захвата. Отпустить
			// обязательно и здесь — resolve мог отдать запись из кэша или чужой
			// полёт, не тронув слота; release идемпотентна (limit.Slots),
			// поэтому двойной вызов после ведущего безопасен. Через defer в
			// отдельной функции: паника внутри resolve (сверка канона идёт
			// до захвата слота ведущим и до его собственного defer) иначе
			// унесла бы слот навсегда.
			held := func(context.Context, limit.Slots, bool) (func(), error) { return rel, nil }
			entry, err = func() (*cacheEntry, error) {
				defer rel()
				return h.resolve(ctx, path, held)
			}()
		}
		if err != nil {
			return nil, err
		}
		switch {
		case entry.notFound:
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		case entry.redirect != "":
			path = entry.redirect
			continue
		case entry.contentType != textContentType:
			return nil, ErrUnknownPath
		}
		return &TextResult{Path: path, Body: entry.body, Canonical: entry.canonical}, nil
	}
	return nil, fmt.Errorf("seo: петля перенаправлений на %s", path)
}

// ErrBusy — ограничитель тяжёлых рендеров занят. Отдельный признак, не
// ErrNotFound: writeRenderError должен ответить на него 503 с Retry-After, а
// не 410.
var ErrBusy = errors.New("читальня занята")

// textContentType — тип ответа текста для нейросетей (.md, llms.txt).
const textContentType = "text/plain; charset=utf-8"

// ErrUnknownPath — путь не узнан таблицей маршрутов (match) или это не текст.
// Page отвечает на него 404, Text отдаёт вызывающему.
var ErrUnknownPath = errors.New("адрес не распознан")

// AcquireFunc — захват тяжёлого слота. Зовётся, только когда рендер нужен на
// самом деле: готовая запись из кэша и лёгкие маршруты слота не занимают.
// wait — вправе ли захват ждать: внутри полёта всегда false (см. Text).
// Возвращаемая release обязана быть идемпотентной: Text рассчитывает на
// это — отложенный вызов ведущего и собственный вызов Text могут сработать оба.
type AcquireFunc func(ctx context.Context, heavy limit.Slots, wait bool) (release func(), err error)

// tryHeavy — захват краулера: без ожидания, wait не смотрит. Краулер ждать не
// может — отвалится по таймауту, а работа продолжится вхолостую.
func tryHeavy(_ context.Context, heavy limit.Slots, _ bool) (func(), error) {
	if rel, ok := heavy.TryAcquire(); ok {
		return rel, nil
	}
	return nil, ErrBusy
}

// writeRenderError печатает 410 на отсутствие сущности, 503 с Retry-After на
// занятый ограничитель и 500 на всё остальное. Общий для ведущего рендера и
// для тех, кто ждал его результата.
//
// 410, а не 404, и это не оттенок. Сюда попадают только адреса ИЗВЕСТНОЙ
// формы (форму не узнал — 404 из ветки match выше, и его перехватывает nginx
// фронта, уводя /help и статику в SPA). Отдать 404 отсюда — значит попасть в
// тот же перехват: краулер получит оболочку SPA с кодом 200, то есть мягкий
// 404, который Гугл либо заведёт в индекс мусором, либо пометит как soft 404.
// 410 перехвата не касается (error_page на него не заведён) и говорит ровно
// то, что надо: 404 поисковик перепроверяет неделями, 410 выбрасывает сразу.
// То же правило выбрано для снятого тома (решение 15).
//
// ErrNeverPublished — намеренное исключение из этого правила: черновик
// подборки никогда не индексировался (в карте сайта его никогда не было), и
// нужен ему не хороший 410, а именно тот самый мягкий перехват в SPA — тот
// же ответ, что и на неизвестную форму адреса. Иначе сам факт существования
// черновика утёк бы через код ответа (410 значит «было и снято»).
func writeRenderError(w http.ResponseWriter, r *http.Request, path string, err error) {
	switch {
	case errors.Is(err, errNoSuchPart):
		// Форма адреса верна, части нет — 404, а не 410: снимать тут нечего.
		http.NotFound(w, r)
	case errors.Is(err, ErrNoSuchRubric):
		http.Error(w, noSuchRubricText, http.StatusNotFound)
	case errors.Is(err, ErrNeverPublished):
		// Отдельная ветка ДО ErrNotFound: черновик существует, но выдать это
		// постороннему через 410 («было и снято») нельзя — см. ErrNeverPublished.
		http.NotFound(w, r)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "Такой страницы в читальне нет", http.StatusGone)
	case errors.Is(err, ErrBusy):
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
		http.Error(w, site.Name()+" занята, зайдите позже", http.StatusServiceUnavailable)
	default:
		log.Printf("seo %s: %v", path, err)
		http.Error(w, "Не удалось собрать страницу", http.StatusInternalServerError)
	}
}

type cacheEntry struct {
	body string
	etag string
	// redirect — не пустая строка, если этот путь неканонический: e.body и
	// e.etag тогда не заполнены, а writeCached отвечает 301 вместо тела.
	redirect string
	// contentType — пусто у HTML-страниц; у текста для нейросетей — свой.
	// Кэш у них общий, и тип обязан ехать с записью.
	contentType string
	// robotsTag и link — заголовки X-Robots-Tag и Link текста (noindex и
	// canonical, которые HTML-страница несёт в <head>). Пусто — заголовка нет.
	robotsTag string
	link      string
	// canonical — абсолютный адрес HTML-страницы текста (td.Canonical). Link
	// собран из него же; отдельно — для Text, которому нужен сам адрес.
	canonical string
	// notFound — отрицательная запись: номер части за пределами главы или
	// подрубрика, которой у понятия нет. writeCached отвечает на неё 404, не
	// трогая остальных полей; notFoundText — текст отказа, пусто — штатный.
	notFound     bool
	notFoundText string
}

// noSuchRubricText — отказ на подрубрику, которой у понятия нет: модели
// надо сказать, почему нет текста, а не отдать голое «404 page not found».
const noSuchRubricText = "Такой подрубрики у этого понятия нет"

func writeCached(w http.ResponseWriter, r *http.Request, e *cacheEntry) {
	if e.notFound {
		if e.notFoundText != "" {
			http.Error(w, e.notFoundText, http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
		return
	}
	if e.redirect != "" {
		// Строка запроса в кэш не попадает: ссылка из поиска несёт ?q= с
		// подсветкой, и Location собирается из живого запроса, а не из записи.
		// Канон со своей строкой (подрубрика понятия, rubricQuery) уже несёт
		// то, что значит, — живая добавила бы её второй раз.
		loc := e.redirect
		if r.URL.RawQuery != "" && !strings.Contains(loc, "?") {
			loc += "?" + r.URL.RawQuery
		}
		w.Header().Set("Location", loc)
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", listMaxAge))
		w.WriteHeader(http.StatusMovedPermanently)
		return
	}
	if e.etag != "" {
		w.Header().Set("ETag", e.etag)
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, e.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	} else {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", listMaxAge))
	}
	ct := e.contentType
	if ct == "" {
		ct = "text/html; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	if e.robotsTag != "" {
		w.Header().Set("X-Robots-Tag", e.robotsTag)
	}
	if e.link != "" {
		w.Header().Set("Link", e.link)
	}
	if _, err := w.Write([]byte(e.body)); err != nil {
		log.Printf("seo: отдача прервана: %v", err)
	}
}

// build — отметка сборки для ETag. nil-источник допустим: так собран
// обработчик в части тестов.
func (h *Handler) build() string {
	if h.src == nil {
		return ""
	}
	return h.src.Build
}

// textEntry — запись кэша для текста: тип, noindex и canonical заголовками.
func textEntry(path, build string, td *TextDoc) *cacheEntry {
	e := &cacheEntry{body: td.Body, contentType: textContentType, canonical: td.Canonical}
	if td.NoIndex {
		e.robotsTag = "noindex"
	}
	if td.Canonical != "" {
		e.link = "<" + td.Canonical + `>; rel="canonical"`
	}
	if !td.CacheKey.IsZero() {
		e.etag = docETag(path, build, td.CacheKey.Unix())
	}
	return e
}

// docETag — ETag страницы: адрес, отметка сборки и метка правки материала.
// Сборка входит наравне с меткой правки: вёрстка меняется и с кодом, а
// краулер с If-None-Match иначе получал бы 304 на страницу прежнего рендерера,
// пока не поправят сам текст (тот же довод, что у COMMIT файлового кэша глав).
func docETag(path, build string, stamp int64) string {
	sum := sha256.Sum256([]byte(path + "|" + build + "|" + strconv.FormatInt(stamp, 10)))
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// route — что делать с путём. heavy отмечает рендеры, стоящие как выгрузка:
// только у них есть ограничитель.
type route struct {
	heavy bool
	// canonical — канонический путь этого адреса, добытый лёгкими запросами
	// (строка работы, строка главы). Стоит отдельно от render намеренно: на
	// нём держится 301 до занятия слота — см. TestHandlerRedirectDoesNotRenderChapter.
	// Пустая функция (nil) означает «у этого вида канона нет» (полосы и режим
	// чтения: они noindex, а canonical у них указывает на главу).
	canonical func(ctx context.Context, s *Source) (string, error)
	render    func(ctx context.Context, s *Source) (*Doc, error)
	// text — рендер простого текста (render_llm.go) вместо HTML. Ровно одно
	// из render, text и textParts непусто.
	text func(ctx context.Context, s *Source) (*TextDoc, error)
	// textParts — текст главы: все части одним рендером (путь главы и части
	// по порядку), part — номер запрошенной, с единицы. Page кладёт в кэш
	// каждую часть, а не только запрошенную — см. ChapterTextParts.
	textParts func(ctx context.Context, s *Source) (string, []*TextDoc, error)
	part      int
	// query — хвост ключей частей в кэше («?rubric_path=…» у подрубрики
	// понятия, rubricQuery); пусто у всех прочих текстов.
	query string
}

// match разбирает путь читальни. Ручной разбор, а не mux: маршруты SPA
// заданы одним деревом в App.tsx, и одна таблица напротив него читается
// целиком, тогда как десяток регистраций в router.go — нет.
func match(path string) (route, bool) {
	if path == "/llms.txt" {
		return route{text: func(ctx context.Context, s *Source) (*TextDoc, error) {
			return s.LLMsText(ctx)
		}}, true
	}
	// Строку запроса в путь кладёт только Page, и только подрубрику понятия
	// (rubricQuery); любой другой хвост — не наш адрес.
	path, query, hasQuery := strings.Cut(path, "?")
	if hasQuery {
		v, err := url.ParseQuery(query)
		rest, isText := strings.CutSuffix(path, ".md")
		if err != nil || !isText || len(v) != 1 {
			return route{}, false
		}
		rubric := ParseRubricPath(v.Get("rubric_path"))
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if rubric == nil || parts[0] != "concepts" {
			return route{}, false
		}
		return matchConceptText(parts[1:], rubric)
	}
	if rest, ok := strings.CutSuffix(path, ".md"); ok {
		return matchText(rest)
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.Home(ctx)
		}}, true
	}

	num := func(i int) (int64, bool) { return leadingID(parts[i]) }

	switch {
	case len(parts) == 2 && parts[0] == "works":
		id, ok := num(1)
		if !ok {
			return route{}, false
		}
		return route{
			canonical: func(ctx context.Context, s *Source) (string, error) {
				return canonicalWork(ctx, s, id)
			},
			render: func(ctx context.Context, s *Source) (*Doc, error) {
				return s.Work(ctx, id)
			},
		}, true

	case len(parts) == 4 && parts[0] == "works" && parts[2] == "chapters":
		workID, ok1 := num(1)
		chapterID, ok2 := num(3)
		if !ok1 || !ok2 {
			return route{}, false
		}
		return route{
			heavy: true,
			canonical: func(ctx context.Context, s *Source) (string, error) {
				return canonicalChapter(ctx, s, workID, chapterID)
			},
			render: func(ctx context.Context, s *Source) (*Doc, error) {
				return s.Chapter(ctx, workID, chapterID)
			},
		}, true

	// «read» и «pages» ведут на одну и ту же полосу разными адресами: первый
	// — режим чтения, второй — карточка полосы. Оба noindex, оба нужны для
	// превью в мессенджере.
	case len(parts) == 4 && parts[0] == "works" && (parts[2] == "read" || parts[2] == "pages"):
		workID, ok1 := num(1)
		page, ok2 := num(3)
		if !ok1 || !ok2 {
			return route{}, false
		}
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.ReadPage(ctx, workID, int(page))
		}}, true

	case len(parts) == 2 && parts[0] == "editions":
		id, ok := num(1)
		if !ok {
			return route{}, false
		}
		return route{
			canonical: func(ctx context.Context, s *Source) (string, error) {
				return canonicalEdition(ctx, s, id)
			},
			render: func(ctx context.Context, s *Source) (*Doc, error) {
				return s.Edition(ctx, id)
			},
		}, true

	case len(parts) == 1 && parts[0] == "concepts":
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.ConceptList(ctx)
		}}, true

	case len(parts) == 2 && parts[0] == "concepts":
		slug := parts[1]
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.Concept(ctx, slug)
		}}, true

	case len(parts) == 1 && parts[0] == "collections":
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.CollectionList(ctx)
		}}, true

	// Первый, короткий адрес — всегда сотрудническая подборка (пустой ник).
	case len(parts) == 2 && parts[0] == "collections":
		slug := parts[1]
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.Collection(ctx, "", slug)
		}}, true

	// Второй, более длинный адрес — читательская подборка: слаг у неё
	// уникален только в паре с ником автора. RobotsNoIndex Source.Collection
	// проставляет сама по непустому нику — здесь этого решать не надо.
	case len(parts) == 3 && parts[0] == "collections":
		nickname, slug := parts[1], parts[2]
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.Collection(ctx, nickname, slug)
		}}, true

	case len(parts) == 1 && parts[0] == "documents":
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.DocumentList(ctx)
		}}, true

	// Первый, короткий адрес — всегда сотруднический разбор (пустая подпись).
	// canonical у разбора нет ни в одном из двух видов: слаг лепит сервер из
	// заглавия при создании и больше не меняет, так что неканонического вида
	// адреса не существует и сверять нечего.
	//
	// heavy: страница разбора собирает корпусные вклейки и стоит как рендер
	// главы — отдавать её мимо ограничителя значило бы отдать боевому
	// двухъядерному серверу столько рендеров, сколько краулеров придёт.
	case len(parts) == 2 && parts[0] == "documents":
		slug := parts[1]
		return route{heavy: true, render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.Document(ctx, "", slug)
		}}, true

	// Второй, более длинный адрес — читательский разбор: слаг у него
	// уникален только в паре с подписью автора.
	case len(parts) == 3 && parts[0] == "documents":
		nickname, slug := parts[1], parts[2]
		return route{heavy: true, render: func(ctx context.Context, s *Source) (*Doc, error) {
			return s.Document(ctx, nickname, slug)
		}}, true

	// Чтение пункта подборки показывает тот же состав, что и сама подборка.
	// Отдельного тела ему не нужно, но и в индекс он не идёт — это дубль под
	// вторым адресом; превью читателю при этом нужно. Только сотруднический
	// вид (4 сегмента, короткий адрес) — у читательской подборки такого дубля
	// нет, это вне брифа задачи 13.
	case len(parts) == 4 && parts[0] == "collections" && parts[2] == "read":
		slug := parts[1]
		return route{render: func(ctx context.Context, s *Source) (*Doc, error) {
			doc, err := s.Collection(ctx, "", slug)
			if err != nil {
				return nil, err
			}
			doc.Robots = RobotsNoIndex
			return doc, nil
		}}, true
	}

	return route{}, false
}

// docCache — обычный LRU с потолком в байтах и сроком годности записи. Свой,
// а не библиотека: нужны два метода, и зависимость ради них в vendor не
// тащим.
type docCache struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	size  int
	order *list.List // спереди — самые свежие
	items map[string]*list.Element
	// gen — поколение содержимого, растёт на каждом сбросе (purge.go).
	// По нему putIfFresh отличает запись, начатую до сброса, от начатой после.
	gen uint64
}

type cacheRecord struct {
	key   string
	entry *cacheEntry
	born  time.Time
}

// entryWeight — вес записи в счётчике c.size. Единая функция для put (вставка
// и вытеснение старых записей) и get (выброс просроченной записи): если бы
// формула считалась в трёх местах по отдельности, разъезд одной из копий
// увёл бы c.size от истины молча — счётчик не сверяется ни с чем внешним.
// Ключ входит в вес наравне с телом: у записи-перенаправления ключ — это
// весь произвольный запрошенный путь (см. cacheEntry.redirect), и без этого
// слагаемого запись всё ещё могла бы притвориться дешёвой при длинном ключе
// и пустых body/redirect.
func entryWeight(key string, entry *cacheEntry) int {
	return len(key) + len(entry.body) + len(entry.redirect) + len(entry.link) + len(entry.canonical) + cacheEntryOverhead
}

func newDocCache(max int, ttl time.Duration) *docCache {
	return &docCache{max: max, ttl: ttl, order: list.New(), items: map[string]*list.Element{}}
}

func (c *docCache) get(key string) (*cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	rec := el.Value.(*cacheRecord)
	if time.Since(rec.born) > c.ttl {
		c.size -= entryWeight(rec.key, rec.entry)
		c.order.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.order.MoveToFront(el)
	return rec.entry, true
}

func (c *docCache) put(key string, entry *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.size -= entryWeight(key, el.Value.(*cacheRecord).entry)
		c.order.Remove(el)
		delete(c.items, key)
	}

	c.items[key] = c.order.PushFront(&cacheRecord{key: key, entry: entry, born: time.Now()})
	c.size += entryWeight(key, entry)

	for c.size > c.max && c.order.Len() > 1 {
		oldest := c.order.Back()
		rec := oldest.Value.(*cacheRecord)
		c.size -= entryWeight(rec.key, rec.entry)
		c.order.Remove(oldest)
		delete(c.items, rec.key)
	}
}

// leadingID — ключ сегмента вида «49-lenin-t06»: ведущее целое, хвост после
// дефиса игнорируется. Сегмент, не начинающийся цифрой, — не адрес.
func leadingID(seg string) (int64, bool) {
	end := 0
	for end < len(seg) && seg[end] >= '0' && seg[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	if end < len(seg) && seg[end] != '-' {
		return 0, false
	}
	v, err := strconv.ParseInt(seg[:end], 10, 64)
	return v, err == nil && v > 0
}

// LeadingID — leadingID для MCP-сервера: id его адресов — пути читальни.
func LeadingID(seg string) (int64, bool) { return leadingID(seg) }

// matchText — адреса текста для нейросетей (render_llm.go): путь уже без
// «.md». Канон — тот же, что у HTML-страницы, плюс хвост текста: 301 на него
// идёт лёгким запросом до слота, как у страниц. Сюда же — понятия указателя
// (`/concepts/…`, render_concept_llm.go).
func matchText(path string) (route, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if parts[0] == "concepts" {
		return matchConceptText(parts[1:], nil)
	}
	if len(parts) < 2 || parts[0] != "works" {
		return route{}, false
	}
	workID, ok := leadingID(parts[1])
	if !ok {
		return route{}, false
	}

	if len(parts) == 2 {
		return route{
			canonical: func(ctx context.Context, s *Source) (string, error) {
				p, err := canonicalWork(ctx, s, workID)
				return p + ".md", err
			},
			text: func(ctx context.Context, s *Source) (*TextDoc, error) {
				return s.WorkText(ctx, workID)
			},
		}, true
	}

	if parts[2] != "chapters" || (len(parts) != 4 && len(parts) != 5) {
		return route{}, false
	}
	chapterID, ok := leadingID(parts[3])
	if !ok {
		return route{}, false
	}
	part := 1
	if len(parts) == 5 {
		n, ok := parsePartSegment(parts[4])
		if !ok {
			return route{}, false
		}
		part = n
	}
	return route{
		heavy: true,
		canonical: func(ctx context.Context, s *Source) (string, error) {
			p, err := canonicalChapter(ctx, s, workID, chapterID)
			return chapterTextPath(p, part), err
		},
		textParts: func(ctx context.Context, s *Source) (string, []*TextDoc, error) {
			return s.ChapterTextParts(ctx, workID, chapterID)
		},
		part: part,
	}, true
}

// parsePartSegment — «part-N», N ≥ 1, только цифры.
func parsePartSegment(seg string) (int, bool) {
	digits, ok := strings.CutPrefix(seg, "part-")
	if !ok || digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// shelfTextRoute — часть part витрины указателя. Лёгкая: без слота.
func shelfTextRoute(part int) route {
	return route{
		canonical: func(context.Context, *Source) (string, error) {
			return chapterTextPath("/concepts", part), nil
		},
		textParts: func(ctx context.Context, s *Source) (string, []*TextDoc, error) {
			return s.ConceptShelfTextParts(ctx)
		},
		part: part,
	}
}

// matchConceptText — текст понятия: /concepts/{слаг}[/part-N]. rest — путь
// после «concepts», уже без «.md». Канона по базе нет — слаг и есть ключ;
// canonical нужен ради 301 с /part-1.md. rubric — подрубрика (?rubric_path=)
// или nil: у витрины её не бывает, у понятия она сужает текст и входит
// хвостом в канон и в ключи частей.
func matchConceptText(rest []string, rubric []string) (route, bool) {
	// Витрина: /concepts.md и /concepts/part-N.md. Разбирается раньше
	// понятия: «part-2» слагом понятия быть не может (транслитерация
	// заглавия), но порядок держит это явно.
	if len(rest) == 0 {
		return shelfTextRoute(1), rubric == nil
	}
	if len(rest) == 1 {
		if n, ok := parsePartSegment(rest[0]); ok {
			return shelfTextRoute(n), rubric == nil
		}
	}
	if len(rest) > 2 || rest[0] == "" {
		return route{}, false
	}
	slug, part := rest[0], 1
	if len(rest) == 2 {
		n, ok := parsePartSegment(rest[1])
		if !ok {
			return route{}, false
		}
		part = n
	}
	base, query := conceptBase(slug), rubricQuery(rubric)
	return route{
		heavy: true,
		canonical: func(context.Context, *Source) (string, error) {
			return chapterTextPath(base, part) + query, nil
		},
		textParts: func(ctx context.Context, s *Source) (string, []*TextDoc, error) {
			return s.ConceptTextParts(ctx, slug, rubric)
		},
		query: query,
		part:  part,
	}, true
}
