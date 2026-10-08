package seo

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"proofreader/internal/limit"
	"proofreader/internal/models"
)

// panicChapters — глава, у которой сверка канона паникует, когда взведён armed.
type panicChapters struct {
	ChapterSource
	armed *bool
}

func (p panicChapters) GetByID(ctx context.Context, id int64) (*models.Chapter, error) {
	if *p.armed {
		panic("источник упал")
	}
	return p.ChapterSource.GetByID(ctx, id)
}

// Паника во втором resolve (слот уже взят снаружи полёта) не должна унести
// слот: после неё занят только слот от заполнения.
func TestTextPanicInSecondResolveFreesHeldSlot(t *testing.T) {
	src := textSource()
	armed := false
	src.Chapters = panicChapters{ChapterSource: src.Chapters, armed: &armed}
	h := handlerFor(src)
	fillHeavy(h)
	acquire := func(ctx context.Context, heavy limit.Slots, wait bool) (func(), error) {
		if !wait {
			return tryHeavy(ctx, heavy, false)
		}
		<-heavy
		armed = true // первый resolve позади, второй упадёт
		rel, ok := heavy.TryAcquire()
		if !ok {
			return nil, ErrBusy
		}
		return rel, nil
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("паника не дошла до вызывающего")
			}
		}()
		h.Text(context.Background(), llmChapter+".md", acquire)
	}()
	if got := len(h.heavy); got != cap(h.heavy)-1 {
		t.Errorf("после паники занято слотов %d, ожидалось %d — слот утёк", got, cap(h.heavy)-1)
	}
}

// waitingAcquire — захват MCP-сервера в миниатюре: без права ждать — попытка,
// с правом — ожидание до patience. entered, если не nil, закрывается при
// первом ожидающем вызове.
func waitingAcquire(patience time.Duration, entered chan struct{}) AcquireFunc {
	var once sync.Once
	return func(ctx context.Context, heavy limit.Slots, wait bool) (func(), error) {
		if !wait {
			return tryHeavy(ctx, heavy, false)
		}
		if entered != nil {
			once.Do(func() { close(entered) })
		}
		if rel, ok := heavy.Acquire(ctx, patience); ok {
			return rel, nil
		}
		return nil, ErrBusy
	}
}

func fillHeavy(h *Handler) {
	for i := 0; i < cap(h.heavy); i++ {
		h.heavy <- struct{}{}
	}
}

func TestTextFollowsCanonical(t *testing.T) {
	h := handlerFor(textSource())
	res, err := h.Text(context.Background(), "/works/1/chapters/10.md", tryHeavy)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != llmChapter+".md" {
		t.Errorf("Path %q, ожидался канон %q", res.Path, llmChapter+".md")
	}
	if res.Canonical != "https://lib.example.org"+llmChapter {
		t.Errorf("Canonical %q", res.Canonical)
	}
	if !strings.Contains(res.Body, "Классовое общество и государство.") {
		t.Errorf("нет текста главы:\n%s", res.Body)
	}
}

// Смена слага в пределах срока кэша: устаревшее перенаправление (голый номер
// -> прежний слаг) ведёт на путь, который сам перенаправляет на канон. Два
// перехода — штатный случай, а не петля.
func TestTextFollowsTwoRedirects(t *testing.T) {
	h := handlerFor(textSource())
	h.cache.put("/works/7.md", &cacheEntry{redirect: "/works/8.md"})
	h.cache.put("/works/8.md", &cacheEntry{redirect: llmChapter + ".md"})
	res, err := h.Text(context.Background(), "/works/7.md", tryHeavy)
	if err != nil || res.Path != llmChapter+".md" {
		t.Fatalf("два перехода: %+v, %v — ожидался канон", res, err)
	}
}

// Третий переход подряд — уже петля.
func TestTextRejectsThreeRedirectsAsLoop(t *testing.T) {
	h := handlerFor(textSource())
	h.cache.put("/works/7.md", &cacheEntry{redirect: "/works/8.md"})
	h.cache.put("/works/8.md", &cacheEntry{redirect: "/works/9.md"})
	h.cache.put("/works/9.md", &cacheEntry{redirect: llmChapter + ".md"})
	res, err := h.Text(context.Background(), "/works/7.md", tryHeavy)
	if err == nil || !strings.Contains(err.Error(), "петля") {
		t.Errorf("три перехода: %+v, %v — ожидалась петля", res, err)
	}
}

// Клиент ушёл, пока ждал слота вне полёта: ошибка контекста не становится
// ErrBusy (по нему в журнале MCP считаются отказы).
func TestTextCancelledWaitIsNotBusy(t *testing.T) {
	h := handlerFor(textSource())
	fillHeavy(h)
	ctx, cancel := context.WithCancel(context.Background())
	acquire := func(ctx context.Context, heavy limit.Slots, wait bool) (func(), error) {
		if !wait {
			return tryHeavy(ctx, heavy, false)
		}
		cancel()
		return nil, ctx.Err()
	}
	_, err := h.Text(ctx, llmChapter+".md", acquire)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrBusy) {
		t.Fatalf("ожидалась context.Canceled без ErrBusy: %v", err)
	}
}

func TestTextServesLLMsTxtAndWorkTOC(t *testing.T) {
	h := handlerFor(llmVolumeSource())
	res, err := h.Text(context.Background(), "/llms.txt", tryHeavy)
	if err != nil || res.Path != "/llms.txt" || res.Canonical != "" {
		t.Fatalf("llms.txt: %+v, %v", res, err)
	}
	res, err = h.Text(context.Background(), "/works/1.md", tryHeavy)
	if err != nil || res.Path != "/works/1-lenin-t42.md" {
		t.Fatalf("оглавление тома: %+v, %v", res, err)
	}
}

func TestTextMissingIsNotFound(t *testing.T) {
	h := handlerFor(longSource())
	for _, p := range []string{llmChapter + "/part-9.md", "/works/999999.md", "/works/1/chapters/999999.md"} {
		if _, err := h.Text(context.Background(), p, tryHeavy); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v, ожидался ErrNotFound", p, err)
		}
	}
}

func TestTextRejectsNonTextPath(t *testing.T) {
	h := handlerFor(textSource())
	for _, p := range []string{"/works/1", llmChapter, "/works/abc.md"} {
		if _, err := h.Text(context.Background(), p, tryHeavy); !errors.Is(err, ErrUnknownPath) {
			t.Errorf("%s: %v, ожидался ErrUnknownPath", p, err)
		}
	}
}

// Не-текст отсекается до resolve: HTML главы (тяжёлый маршрут) иначе занял бы
// слот ради ответа, который Text всё равно выбросит.
func TestTextNonTextPathSkipsAcquire(t *testing.T) {
	h := handlerFor(textSource())
	for _, p := range []string{"/works/1", llmChapter} {
		called := false
		spy := func(context.Context, limit.Slots, bool) (func(), error) {
			called = true
			return nil, ErrBusy
		}
		if _, err := h.Text(context.Background(), p, spy); !errors.Is(err, ErrUnknownPath) {
			t.Errorf("%s: %v, ожидался ErrUnknownPath", p, err)
		}
		if called {
			t.Errorf("%s: не-текст занял тяжёлый слот", p)
		}
	}
}

// Краулер слота не ждёт (tryHeavy — ErrBusy); MCP ждёт через свой acquire —
// и, дождавшись, рендерит.
func TestTextAcquireDecidesWhetherToWait(t *testing.T) {
	h := handlerFor(textSource())
	fillHeavy(h)
	if _, err := h.Text(context.Background(), llmChapter+".md", tryHeavy); !errors.Is(err, ErrBusy) {
		t.Fatalf("без ожидания при занятых слотах: %v, ожидался ErrBusy", err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		<-h.heavy
	}()
	res, err := h.Text(context.Background(), llmChapter+".md", waitingAcquire(time.Second, nil))
	if err != nil {
		t.Fatalf("с ожиданием: %v", err)
	}
	if !strings.Contains(res.Body, "Классовое общество и государство.") {
		t.Errorf("нет текста главы:\n%s", res.Body)
	}
	// Слот, взятый снаружи полёта, отпущен: занят только тот, что остался
	// от заполнения.
	if got := len(h.heavy); got != cap(h.heavy)-1 {
		t.Errorf("после Text занято слотов %d, ожидалось %d", got, cap(h.heavy)-1)
	}
}

// Пока MCP ждал слота, главу отрендерил краулер: resolve со взятым слотом
// отдаёт запись из кэша, слота не тронув, — Text обязан отпустить его сам.
func TestTextHeldSlotReleasedOnCacheHit(t *testing.T) {
	h := handlerFor(textSource())
	fillHeavy(h)
	acquire := func(ctx context.Context, heavy limit.Slots, wait bool) (func(), error) {
		if !wait {
			return tryHeavy(ctx, heavy, false)
		}
		<-heavy // слот освободился — первым его взял краулер
		if rec := get(t, h, llmChapter+".md", nil); rec.Code != http.StatusOK {
			t.Fatalf("краулер: код %d", rec.Code)
		}
		if rel, ok := heavy.TryAcquire(); ok {
			return rel, nil
		}
		return nil, ErrBusy
	}
	if _, err := h.Text(context.Background(), llmChapter+".md", acquire); err != nil {
		t.Fatal(err)
	}
	if got := len(h.heavy); got != cap(h.heavy)-1 {
		t.Errorf("после Text занято слотов %d, ожидалось %d — взятый слот не отпущен", got, cap(h.heavy)-1)
	}
}

// MCP и краулер по одному адресу .md делят полёт. Пока MCP ждёт слота,
// краулер обязан сразу получить 503, а не ждать вместе с ним.
func TestTextWaitDoesNotHoldCrawlerOnSamePath(t *testing.T) {
	h := handlerFor(textSource())
	fillHeavy(h)

	entered := make(chan struct{})
	textDone := make(chan error, 1)
	go func() {
		_, err := h.Text(context.Background(), llmChapter+".md", waitingAcquire(2*time.Second, entered))
		textDone <- err
	}()
	<-entered

	pageDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { pageDone <- get(t, h, llmChapter+".md", nil) }()
	select {
	case rec := <-pageDone:
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Errorf("краулер: код %d, Retry-After %q — ожидался 503 с Retry-After",
				rec.Code, rec.Header().Get("Retry-After"))
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("краулер ждёт тяжёлого слота вместе с MCP")
	}

	<-h.heavy
	if err := <-textDone; err != nil {
		t.Errorf("MCP после освобождения слота: %v", err)
	}
}

// Любой отказ захвата у ведущего — занятость для его ждущих: краулер получает
// 503 с Retry-After, а не 500 из-за чужой ошибки (у MCP-сервера она своя).
func TestLeadAcquireFailureIsBusyForFollowers(t *testing.T) {
	h := handlerFor(textSource())
	entered := make(chan struct{})
	proceed := make(chan struct{})
	failing := func(context.Context, limit.Slots, bool) (func(), error) {
		close(entered)
		<-proceed
		return nil, errors.New("ворота MCP не дождались")
	}
	leaderDone := make(chan error, 1)
	go func() {
		_, err := h.resolve(context.Background(), llmChapter+".md", failing)
		leaderDone <- err
	}()
	<-entered

	// Слоты свободны: опоздавший краулер стал бы ведущим сам и получил бы
	// 200 — тест упадёт, а не пройдёт мимо.
	pageDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { pageDone <- get(t, h, llmChapter+".md", nil) }()
	time.Sleep(10 * time.Millisecond)
	close(proceed)

	if rec := <-pageDone; rec.Code != http.StatusServiceUnavailable {
		t.Errorf("ждущий краулер: код %d, ожидался 503; тело: %s", rec.Code, rec.Body.String())
	}
	if err := <-leaderDone; !errors.Is(err, ErrBusy) {
		t.Errorf("ведущий: %v, ожидался ErrBusy", err)
	}
}

// Краулер ушёл, а ведущий сломался (рендер идёт под WithoutCancel, его ошибка
// — не обрыв): ответа нет, но поломка в журнале.
func TestPageGoneCrawlerStillLogsBreakage(t *testing.T) {
	src := volumeSource()
	src.Works.(*fakeWorks).err = errors.New("failed to query: база легла")
	h := handlerFor(src)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/seo/works/1", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Page(rec, req)

	if !strings.Contains(buf.String(), "база легла") {
		t.Errorf("поломка ведущего не попала в журнал: %q", buf.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("ушедшему краулеру что-то написано: %q", rec.Body.String())
	}
}

// Готовая часть слота не занимает: acquire не зовётся вовсе.
func TestTextFromCacheSkipsAcquire(t *testing.T) {
	h := handlerFor(textSource())
	if _, err := h.Text(context.Background(), llmChapter+".md", tryHeavy); err != nil {
		t.Fatal(err)
	}
	called := false
	spy := func(context.Context, limit.Slots, bool) (func(), error) {
		called = true
		return nil, ErrBusy
	}
	if _, err := h.Text(context.Background(), llmChapter+".md", spy); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("часть из кэша заняла тяжёлый слот")
	}
}

// Снятие тома: сброс кэша и отказ источника — и MCP больше не видит текста.
func TestTextAfterTakedownIsNotFound(t *testing.T) {
	src := longSource()
	h := handlerFor(src)
	if _, err := h.Text(context.Background(), llmChapter+"/part-2.md", tryHeavy); err != nil {
		t.Fatal(err)
	}
	delete(src.Works.(*fakeWorks).byID, 1)
	h.Purge()
	for _, p := range []string{llmChapter + ".md", llmChapter + "/part-3.md", "/works/1-lenin-t42.md"} {
		if _, err := h.Text(context.Background(), p, tryHeavy); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s после снятия: %v, ожидался ErrNotFound", p, err)
		}
	}
}
