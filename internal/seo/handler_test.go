package seo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func handlerFor(s *Source) *Handler { return NewHandler(s) }

func get(t *testing.T, h *Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/seo"+path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.Page(rec, req)
	return rec
}

func TestHandlerServesWorkCard(t *testing.T) {
	rec := get(t, handlerFor(volumeSource()), "/works/1-lenin-t42", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type: %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `property="og:title"`) {
		t.Errorf("нет og:title:\n%s", rec.Body.String())
	}
}

// Закрытые и неизвестные адреса краулеру отдавать нечего: nginx посылает сюда
// любой его запрос, включая /login и /favicon.ico.
func TestHandlerUnknownPathIs404(t *testing.T) {
	for _, path := range []string{"/login", "/admin/users", "/favicon.ico", "/works/abc"} {
		if rec := get(t, handlerFor(volumeSource()), path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", path, rec.Code)
		}
	}
}

// Повторный обход не должен пересобирать страницу: ETag из CacheKey и 304 —
// то же, чем живут выгрузки.
func TestHandlerAnswers304OnMatchingETag(t *testing.T) {
	h := handlerFor(volumeSource())

	first := get(t, h, "/works/1-lenin-t42", nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag не выставлен")
	}

	second := get(t, h, "/works/1-lenin-t42", map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusNotModified {
		t.Errorf("код %d, ожидался 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("при 304 тело должно быть пустым, получено %d байт", second.Body.Len())
	}
}

// ETag зависит от сборки: вёрстка меняется с кодом, а не только с текстом.
// Без отметки сборки краулер с If-None-Match получал бы 304 на страницу,
// собранную прежним рендерером (без новой строки для нейросетей и
// rel=alternate), пока не поправят сам текст главы. При той же сборке ETag
// обязан совпадать — иначе 304 не случится никогда.
func TestHandlerETagDependsOnBuild(t *testing.T) {
	for _, c := range []struct {
		name string
		src  func() *Source
		etag func(h *Handler) string
	}{
		{"HTML тома", volumeSource, func(h *Handler) string {
			return get(t, h, "/works/1-lenin-t42", nil).Header().Get("ETag")
		}},
		{"текст главы", textSource, func(h *Handler) string {
			return get(t, h, "/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya.md", nil).Header().Get("ETag")
		}},
		{"оглавление тома", llmVolumeSource, func(h *Handler) string {
			return get(t, h, "/works/1-lenin-t42.md", nil).Header().Get("ETag")
		}},
		{"карточка", cardHTTPSource, func(h *Handler) string {
			return ogGet(t, h, "/og/work/1.png", nil).Header().Get("ETag")
		}},
	} {
		withBuild := func(build string) string {
			src := c.src()
			src.Build = build
			return c.etag(handlerFor(src))
		}
		a1, a2, b := withBuild("a1b2c3"), withBuild("a1b2c3"), withBuild("d4e5f6")
		if a1 == "" {
			t.Errorf("%s: ETag не выставлен", c.name)
			continue
		}
		if a1 != a2 {
			t.Errorf("%s: одна сборка, разные ETag: %s и %s", c.name, a1, a2)
		}
		if a1 == b {
			t.Errorf("%s: ETag не зависит от сборки: %s", c.name, a1)
		}
	}
}

// Переполнение ограничителя тяжёлых рендеров — не ошибка, а просьба сбавить
// темп: 503 с Retry-After Гуглобот понимает именно так.
func TestHandlerAnswers503WhenHeavyRendersAreBusy(t *testing.T) {
	h := handlerFor(textSource())

	// Занять все слоты, ничего не отпуская.
	for i := 0; i < cap(h.heavy); i++ {
		h.heavy <- struct{}{}
	}

	rec := get(t, h, "/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("код %d, ожидался 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("нет Retry-After — краулер не узнает, когда вернуться")
	}
}

func TestHandlerReadPageIsNoIndex(t *testing.T) {
	rec := get(t, handlerFor(textSource()), "/works/1/read/5", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `content="noindex,follow"`) {
		t.Errorf("полоса отдана без noindex:\n%s", rec.Body.String())
	}
}

// /collections/{slug}/read/{itemId} отдаёт тот же состав, что и сама
// подборка (для превью в мессенджере), но с RobotsNoIndex — второй адрес
// того же содержимого в индекс не идёт. Это единственное место, где
// проставляется doc.Robots, и без теста на уровне обработчика правка тихо
// пропадёт при следующей редактуре match().
func TestHandlerCollectionItemIsNoIndex(t *testing.T) {
	rec := get(t, handlerFor(indexSource()), "/collections/o-gosudarstve/read/1", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Государство и революция") {
		t.Errorf("нет состава подборки:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `content="noindex,follow"`) {
		t.Errorf("чтение пункта подборки отдано без noindex:\n%s", rec.Body.String())
	}
}

// Второй заход за той же страницей обслуживается из кэша, не тревожа источник.
func TestHandlerCachesRenderedPage(t *testing.T) {
	s := volumeSource()
	h := handlerFor(s)

	get(t, h, "/works/1-lenin-t42", nil)
	// Убрать работу из источника: если кэш работает, страница всё равно
	// отдастся.
	delete(s.Works.(*fakeWorks).byID, 1)

	if rec := get(t, h, "/works/1-lenin-t42", nil); rec.Code != http.StatusOK {
		t.Errorf("вторая выдача не пришла из кэша: код %d", rec.Code)
	}
}

// Разосланная ссылка на свежую главу приходит от нескольких мессенджеров
// почти одновременно: два промаха кэша по одному адресу не должны рендерить
// главу дважды и отбирать оба слота ограничителя друг у друга. Второй запрос
// обязан дождаться результата первого, а не начать свой рендер.
func TestHandlerDedupesConcurrentIdenticalRenders(t *testing.T) {
	s := textSource()
	books := s.Books.(*fakeBooks)
	// Растянуть рендер, чтобы второй запрос гарантированно застал первый в
	// полёте, а не пришёл уже по готовому кэшу.
	books.delay = 50 * time.Millisecond
	h := handlerFor(s)

	var wg sync.WaitGroup
	results := make([]*httptest.ResponseRecorder, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = get(t, h, "/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya", nil)
		}(i)
	}
	wg.Wait()

	for i, rec := range results {
		if rec.Code != http.StatusOK {
			t.Errorf("запрос %d: код %d, тело: %s", i, rec.Code, rec.Body.String())
		}
	}
	if got := atomic.LoadInt32(&books.calls); got != 1 {
		t.Errorf("обращений к источнику: %d, ожидался 1 — глава отрендерена дважды", got)
	}
}

// F5 итогового ревью: рендер ведущего запроса шёл под его же r.Context(). Если
// первый краулер отваливался по таймауту раньше своего рендера — у Телеграма
// окно короткое, а тяжёлые рендеры это как раз главы, — render получал
// context.Canceled, ошибка ложилась в fl.err, и все ждущие получали 500 из-за
// чужого обрыва, хотя дедупликация существует ровно ради общего результата.
func TestHandlerLeaderCancellationDoesNotFailFollowers(t *testing.T) {
	s := textSource()
	books := s.Books.(*fakeBooks)
	books.delay = 50 * time.Millisecond
	h := handlerFor(s)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderReq := httptest.NewRequest(http.MethodGet, "/seo/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya", nil).WithContext(leaderCtx)
	leaderRec := httptest.NewRecorder()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.Page(leaderRec, leaderReq)
	}()

	// Дать ведущему занять слот полёта, прежде чем отменять его контекст:
	// иначе отмена могла бы прийти раньше, чем флаг вообще появится в карте.
	time.Sleep(10 * time.Millisecond)
	cancelLeader()

	followerRec := get(t, h, "/works/1-lenin-t42/chapters/10-gosudarstvo-i-revolyuciya", nil)
	wg.Wait()

	if leaderRec.Code != http.StatusOK {
		t.Errorf("ведущий: код %d, тело: %s", leaderRec.Code, leaderRec.Body.String())
	}
	if followerRec.Code != http.StatusOK {
		t.Errorf("ведомый: код %d, тело: %s — не должен падать из-за отмены чужого контекста", followerRec.Code, followerRec.Body.String())
	}
}

// Кэш без срока годности отдавал бы вчерашний текст неделями на редко
// посещаемом адресе — машинная вычитка правит полосы каждый день. Проверяем
// сам docCache: ничтожный TTL, запись кладём, следующий get должен дать
// промах и выбросить запись (размер вернулся к нулю).
func TestDocCacheExpiresEntries(t *testing.T) {
	c := newDocCache(1<<20, time.Nanosecond)
	c.put("/works/1", &cacheEntry{body: "тело страницы"})

	time.Sleep(time.Microsecond)

	if _, ok := c.get("/works/1"); ok {
		t.Fatal("просроченная запись отдана как попадание")
	}
	if c.size != 0 {
		t.Errorf("размер после выброса просроченной записи: %d, ожидался 0", c.size)
	}
	if c.order.Len() != 0 {
		t.Errorf("список после выброса просроченной записи: %d элементов, ожидалось 0", c.order.Len())
	}
}

// Записи-перенаправления несут пустое тело (cacheEntry.redirect, а не
// .body): до правки веса записи put учитывал только len(entry.body), то есть
// ноль байт — вытеснение по c.size > c.max для них никогда не срабатывало, и
// произвольный поддельный хвост (терпимый разбор путей — задача с этой же
// ветки) заполнял кэш вечными записями без счёта. Двести put с разными
// ключами и одним и тем же перенаправлением воспроизводят живой замер из
// отчёта ревью (200 запросов /seo/works/49-probeN — все осели в кэше).
func TestDocCachePutEvictsOldRedirectEntries(t *testing.T) {
	c := newDocCache(4096, time.Hour)
	const n = 500
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("/works/49-probe%d", i)
		c.put(key, &cacheEntry{redirect: "/works/49-lenin-t06"})
	}
	t.Logf("после %d put(): %d записей в кэше, вес %d/%d байт", n, len(c.items), c.size, c.max)

	if len(c.items) >= n {
		t.Fatalf("ни одна запись-перенаправление не вытеснена: %d записей при потолке %d байт (все %d поместились бы только при нулевом весе записи)", len(c.items), c.max, n)
	}
	if c.size > c.max {
		t.Fatalf("вес кэша превысил потолок: %d > %d", c.size, c.max)
	}
	if got, want := len(c.items), c.order.Len(); got != want {
		t.Fatalf("карта и список разошлись: %d записей в map, %d в списке", got, want)
	}

	newestKey := fmt.Sprintf("/works/49-probe%d", n-1)
	if _, ok := c.get(newestKey); !ok {
		t.Errorf("самая свежая запись (%s) должна была остаться в кэше", newestKey)
	}
	if _, ok := c.get("/works/49-probe0"); ok {
		t.Error("самая старая запись-перенаправление должна была вытесниться, а не остаться навечно")
	}
}

func TestHandlerAcceptsSluggedPaths(t *testing.T) {
	h := handlerFor(volumeSource())
	for _, path := range []string{"/works/1", "/works/1-lenin-t42", "/works/1-старый-слаг"} {
		if rec := get(t, h, path, nil); rec.Code == http.StatusNotFound {
			t.Errorf("%s: 404, адрес обязан разбираться", path)
		}
	}
}

func TestHandlerStillRejectsNonNumericSegment(t *testing.T) {
	h := handlerFor(volumeSource())
	for _, path := range []string{"/works/abc", "/works/-lenin", "/works/lenin-t06", "/works/49abc", "/works/1x"} {
		if rec := get(t, h, path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404 — терпимость касается только хвоста", path, rec.Code)
		}
	}
}

func TestHandlerRedirectsNumericPathToCanonical(t *testing.T) {
	rec := get(t, handlerFor(volumeSource()), "/works/1", nil)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("код %d, ожидался 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/works/1-lenin-t42" {
		t.Errorf("Location = %q", loc)
	}
}

func TestHandlerKeepsQueryOnRedirect(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/seo/works/1?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F", nil)
	rec := httptest.NewRecorder()
	handlerFor(volumeSource()).Page(rec, req)

	want := "/works/1-lenin-t42?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F"
	if loc := rec.Header().Get("Location"); loc != want {
		t.Errorf("Location = %q, ожидалось %q — иначе ссылка из поиска теряет подсветку", loc, want)
	}
}

// Причина, по которой сверка канона стоит выше ограничителя: после выкатки
// весь индекс поисковика — числовые адреса глав, и рендер ради выброшенного
// тела занял бы оба тяжёлых слота на боевом с двумя ядрами.
func TestHandlerRedirectDoesNotRenderChapter(t *testing.T) {
	src := textSource()
	books := src.Books.(*fakeBooks)

	rec := get(t, handlerFor(src), "/works/1/chapters/10", nil)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("код %d, ожидался 301", rec.Code)
	}
	if n := atomic.LoadInt32(&books.calls); n != 0 {
		t.Errorf("тело главы собиралось %d раз(а), ожидалось ноль", n)
	}
}

// Полосы и режим чтения не перенаправляются: у них noindex, а canonical
// указывает на главу, то есть «неканонический вид» для них не определён.
// Лишний прыжок удлинил бы путь превью в Телеграме, у которого окно короткое.
func TestHandlerDoesNotRedirectPageAndReadingViews(t *testing.T) {
	h := handlerFor(textSource())
	for _, path := range []string{"/works/1/pages/5", "/works/1/read/5",
		"/works/1-lenin-t42/pages/5"} {
		if rec := get(t, h, path, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: код %d, ожидался 200", path, rec.Code)
		}
	}
}

// Адрес известной формы, за которым нет сущности, обязан отвечать 410 Gone.
//
// Ни 500, ни 404 здесь не годятся, и оба были попробованы. 500 поисковик
// читает как «читальня сломалась, вернусь позже»: адрес остаётся в очереди, а
// частые 500 роняют темп обхода целиком — то есть бьют по единственному, чего
// у нас мало (тикет 17: Googlebot взял две главы за две недели). А 404 до
// краулера не доходит вовсе: nginx фронта перехватывает его
// (`proxy_intercept_errors on; error_page 404 = @spa`) и отдаёт оболочку SPA
// с кодом 200 — мягкий 404, который Гугл либо заводит в индекс мусором, либо
// метит как soft 404. Перехват заведён не по ошибке: обработчик знает лишь
// девять форм адреса и честно 404-ит на /help и /vite.svg, которым положено
// уехать в SPA, а различить «форму не знаю» и «сущности нет» по одному коду
// нельзя.
//
// 410 через перехват проходит (error_page на него не заведён) и означает
// ровно то, что нужно: 404 поисковик перепроверяет неделями, 410 выбрасывает
// сразу. То же правило выбрано для снятого тома (решение 15).
//
// Форму адреса обработчик по-прежнему не узнаёт с кодом 404 — это ветка выше
// по коду (match), и трогать её нельзя, иначе /help перестанет открываться.
func TestHandlerMissingEntityIsGone(t *testing.T) {
	// catalogSource, а не textSource: у него заполнены издания, и случай
	// /editions/N закрывает третью из трёх лёгких сверок канона.
	h := handlerFor(catalogSource())
	for _, path := range []string{
		"/works/999999",
		"/works/999999/chapters/10",
		"/works/1/chapters/999999",
		"/works/999999/pages/5",
		"/editions/999999",
		// Работа на месте, полосы нет: именно так выглядит ссылка из цитаты
		// на снятую полосу. Соседняя строка выше проверяет отсутствующую
		// РАБОТУ — это другая ветка, и её прохождение ничего не говорит про
		// эту.
		"/works/1/pages/999999",
	} {
		rec := get(t, h, path, nil)
		if rec.Code != http.StatusGone {
			t.Errorf("%s: код %d, ожидался 410", path, rec.Code)
		}
	}
}

// Обратная половина того же: адрес, формы которого обработчик не знает,
// обязан остаться 404 — только его перехватывает nginx и уводит в SPA.
// Стоит 410 здесь — и /help, /vite.svg и всякая статика перестанут
// открываться краулеру вовсе.
func TestHandlerUnknownRouteShapeStaysNotFound(t *testing.T) {
	h := handlerFor(catalogSource())
	for _, path := range []string{"/help", "/vite.svg", "/works/1/reading"} {
		rec := get(t, h, path, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404 (его перехватывает nginx и отдаёт SPA)", path, rec.Code)
		}
	}
}

// Занятый ограничитель не должен мешать перенаправлению: 301 слот не берёт.
func TestHandlerRedirectsWhileHeavyRendersBusy(t *testing.T) {
	h := handlerFor(textSource())
	for i := 0; i < cap(h.heavy); i++ {
		h.heavy <- struct{}{}
	}
	if rec := get(t, h, "/works/1/chapters/10", nil); rec.Code != http.StatusMovedPermanently {
		t.Errorf("код %d, ожидался 301", rec.Code)
	}
}
