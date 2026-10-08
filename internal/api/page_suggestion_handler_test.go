package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type fakeSuggestionStore struct {
	created   []*models.PageSuggestion
	recentIPs map[string]int
	detail    *repository.SuggestionDetail
	rows      []repository.SuggestionRow
	total     int

	listCalled bool
	listStatus *models.PageSuggestionStatus
	listLimit  int
	listOffset int

	resolvedID          int64
	resolvedStatus      models.PageSuggestionStatus
	resolvedReason      *models.RejectReason
	resolvedModeratorID int64
	resolveErr          error

	deletedIDs  []int64
	deleteErr   error
	purgeCalled bool
	purgeCount  int64
	purgeErr    error

	// calledUserID запоминает, с каким userID звали ListByUserID — тесты
	// проверяют, что Mine берёт автора из claims, а не откуда-то ещё.
	calledUserID int64
}

// CreateWithinLimit повторяет договор боевого метода: предел проверяется
// вместе с записью, отбой возвращается как (false, nil), а не ошибкой.
// recentIPs служит и предзасевом счётчика (тест предела), и его состоянием.
func (f *fakeSuggestionStore) CreateWithinLimit(
	ctx context.Context, s *models.PageSuggestion, limit int, since time.Time,
) (bool, error) {
	if f.recentIPs[s.IPHash] >= limit {
		return false, nil
	}
	f.recentIPs[s.IPHash]++
	s.ID = int64(len(f.created) + 1)
	s.Status = models.SuggestionNew
	s.CreatedAt = time.Now()
	f.created = append(f.created, s)
	return true, nil
}

func (f *fakeSuggestionStore) List(ctx context.Context, status *models.PageSuggestionStatus, limit, offset int) ([]repository.SuggestionRow, int, error) {
	f.listCalled = true
	f.listStatus = status
	f.listLimit = limit
	f.listOffset = offset
	return f.rows, f.total, nil
}

func (f *fakeSuggestionStore) ListByUserID(ctx context.Context, userID int64) ([]repository.SuggestionRow, error) {
	f.calledUserID = userID
	return f.rows, nil
}

func (f *fakeSuggestionStore) GetDetail(ctx context.Context, id int64) (*repository.SuggestionDetail, error) {
	if f.detail == nil {
		return nil, fmt.Errorf("no such suggestion")
	}
	return f.detail, nil
}

func (f *fakeSuggestionStore) Resolve(ctx context.Context, id int64, status models.PageSuggestionStatus, reason *models.RejectReason, moderatorID int64) error {
	f.resolvedID, f.resolvedStatus, f.resolvedReason, f.resolvedModeratorID = id, status, reason, moderatorID
	return f.resolveErr
}

const testPageText = "Текст полосы до правки"

// pageStoreWith — подставной склад страниц вокруг одной живой страницы.
//
// fakePageStore в internal/api/stores_test.go устроен на замыканиях
// (getByIDFn, saveEditFn и прочие), а не на срезе: поля pages у него НЕТ.
// SaveEdit оставлен умолчанию — оно запоминает снимок и отвечает успехом, а
// applyPageEdit правит ту же структуру по указателю, поэтому проверки по
// page.ContentMarkdown работают.
//
// callCount, если не nil, считает обращения к GetByID. Так тест ловит
// регрессию порядка операторов: если отбой по управляющим символам переедет
// ниже h.pages.GetByID, счётчик перестанет быть нулём — а без него 422 и
// пустой store.created прошли бы одинаково что до, что после регрессии.
func pageStoreWith(page *models.Page, callCount *int) *fakePageStore {
	return &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			if callCount != nil {
				*callCount++
			}
			if page == nil || id != page.ID {
				return nil, fmt.Errorf("page %d not found", id)
			}
			return page, nil
		},
	}
}

// testReaderClaims — claims читателя, которые в бою кладёт в контекст
// AuthMiddleware на подроутере reader. userID=5 — тот же читатель, что
// используют TestCreateSuggestionTakesAuthorFromClaims и TestMineListsByUser.
var testReaderClaims = &auth.Claims{UserID: 5, Role: models.RoleReader}

// withClaims оборачивает обработчик так же, как это делает AuthMiddleware на
// живом маршруте — подкладывает claims в контекст запроса. claims == nil
// имитирует запрос, дошедший до обработчика без прохода через middleware
// (страховочная ветка внутри Create/Mine).
func withClaims(claims *auth.Claims, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if claims != nil {
			r = r.WithContext(context.WithValue(r.Context(), middleware.UserContextKey, claims))
		}
		fn(w, r)
	}
}

func suggestionTestRig(t *testing.T) (*fakeSuggestionStore, *fakePageStore, http.Handler) {
	t.Helper()
	suggestions := &fakeSuggestionStore{recentIPs: map[string]int{}}
	pages := pageStoreWith(&models.Page{
		ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: testPageText,
	}, nil)
	h := NewPageSuggestionHandler(suggestions, pages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)

	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions",
		withClaims(testReaderClaims, h.Create)).Methods("POST")
	return suggestions, pages, r
}

func postSuggestion(t *testing.T, r http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/works/7/pages/42/suggestions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func validBody() map[string]any {
	return map[string]any{
		"proposed_markdown": "Текст полосы после правки",
		"note":              "опечатка в третьей строке",
		"base_sha256":       pageSHA256(testPageText),
		"binding_ref":       "",
	}
}

func TestSuggestionCreateAccepted(t *testing.T) {
	store, _, r := suggestionTestRig(t)

	rec := postSuggestion(t, r, validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatalf("ожидалась одна запись, создано %d", len(store.created))
	}
	// Основу кладёт сервер из текущего текста, а не берёт из тела запроса.
	if store.created[0].BaseMarkdown != testPageText {
		t.Fatalf("основа %q, ожидалась %q", store.created[0].BaseMarkdown, testPageText)
	}
}

// TestCreateSuggestionTakesAuthorFromClaims — Step 1 брифа: автор правки
// пишется из claims (userID вошедшего читателя), а не из какого-либо поля
// тела запроса — reader_key в теле у Create больше нет вовсе.
func TestCreateSuggestionTakesAuthorFromClaims(t *testing.T) {
	store, _, r := suggestionTestRig(t)

	rec := postSuggestion(t, r, validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatalf("ожидалась одна запись, создано %d", len(store.created))
	}
	got := store.created[0].UserID
	if got == nil || *got != testReaderClaims.UserID {
		t.Fatalf("UserID = %v, ожидался %d (из claims)", got, testReaderClaims.UserID)
	}
}

func TestSuggestionHoneypotAnswersCreatedAndStoresNothing(t *testing.T) {
	store, _, r := suggestionTestRig(t)

	body := validBody()
	body["binding_ref"] = "http://spam.example"

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ловушка обязана отвечать успехом, получено %d", rec.Code)
	}
	if len(store.created) != 0 {
		t.Fatalf("ловушка сработала, но запись создана: %+v", store.created)
	}
}

func TestSuggestionStaleBaseRejected(t *testing.T) {
	_, _, r := suggestionTestRig(t)

	body := validBody()
	body["base_sha256"] = pageSHA256("совсем другой текст")

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("ожидался 409, получено %d", rec.Code)
	}
	// Ошибка обязана быть JSON-объектом: apiError.ts читает data.message.
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("тело ошибки не JSON: %s", rec.Body.String())
	}
	if payload.Message == "" {
		t.Fatal("в теле ошибки нет message — читатель увидит запасную фразу")
	}
}

func TestSuggestionUnchangedTextRejected(t *testing.T) {
	_, _, r := suggestionTestRig(t)

	body := validBody()
	body["proposed_markdown"] = testPageText

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ожидался 422, получено %d", rec.Code)
	}
}

func TestSuggestionRelativeCapRejectsBloat(t *testing.T) {
	// Основа нарочно больше пола (20 000): при testPageText в 22 знака
	// потолок 4×основа+2000 = 2088 целиком перекрывался бы полом, и тест
	// проверял бы уже не множитель, а саму константу пола. Здесь основа в
	// 10 000 знаков даёт потолок 42 000 — заведомо выше пола и заведомо
	// ниже абсолютного предела (200 000), так что срабатывает именно
	// множитель, а не соседние отказы.
	base := strings.Repeat("о", 10000)
	store := &fakeSuggestionStore{recentIPs: map[string]int{}}
	pages := pageStoreWith(&models.Page{
		ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: base,
	}, nil)
	h := NewPageSuggestionHandler(store, pages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions",
		withClaims(testReaderClaims, h.Create)).Methods("POST")

	body := validBody()
	body["base_sha256"] = pageSHA256(base)
	relativeCap := 4*len([]rune(base)) + 2000
	body["proposed_markdown"] = strings.Repeat("м", relativeCap+1)

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ожидался 422, получено %d: %s", rec.Code, rec.Body.String())
	}
	// Отказ обязан называть число потолка, а не просто «слишком велика» —
	// иначе читатель, упёршийся в предел, не понимает, на сколько сократить.
	if !strings.Contains(rec.Body.String(), fmt.Sprint(relativeCap)) {
		t.Fatalf("в отказе нет числа потолка (%d): %s", relativeCap, rec.Body.String())
	}
}

func TestSuggestionAbsoluteCapRejectsEvenWithinRelativeCap(t *testing.T) {
	// Основа большая настолько, что относительный потолок (4×основа+2000)
	// сам оказывается выше абсолютного предела в 200 000 знаков — иначе
	// невозможно отличить «сработал абсолютный предел» от «сработал
	// относительный»: с крошечной testPageText относительный потолок
	// (2 088 или, после пола, 20 000) всегда меньше абсолютного и срабатывает
	// первым, оставляя suggestionMaxChars ничем не проверенным.
	base := strings.Repeat("о", 60000) // относительный потолок 242 000 > 200 000
	store := &fakeSuggestionStore{recentIPs: map[string]int{}}
	pages := pageStoreWith(&models.Page{
		ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: base,
	}, nil)
	h := NewPageSuggestionHandler(store, pages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions",
		withClaims(testReaderClaims, h.Create)).Methods("POST")

	body := validBody()
	body["base_sha256"] = pageSHA256(base)
	// Между абсолютным пределом (200 000) и относительным потолком (242 000)
	// на этой основе: годится только для проверки абсолютного.
	body["proposed_markdown"] = strings.Repeat("м", suggestionMaxChars+1)

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ожидался 422, получено %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.created) != 0 {
		t.Fatal("запись создана при превышении абсолютного предела")
	}
	if !strings.Contains(rec.Body.String(), fmt.Sprint(suggestionMaxChars)) {
		t.Fatalf("в отказе нет числа абсолютного предела (%d): %s",
			suggestionMaxChars, rec.Body.String())
	}
}

func TestSuggestionRelativeCapFloorAllowsNormalTextOnEmptyBase(t *testing.T) {
	// Пустая полоса (708 таких в рабочей базе на 27.08.2026): без пола
	// 4×0+2000 = 2000 обрезает читателя, взявшегося расшифровать скан с
	// нуля, на тексте нормальной для полосы длины (средняя по корпусу —
	// 2137 знаков). Пол в 20 000 обязан такой текст пропускать.
	store := &fakeSuggestionStore{recentIPs: map[string]int{}}
	pages := pageStoreWith(&models.Page{
		ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: "",
	}, nil)
	h := NewPageSuggestionHandler(store, pages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions",
		withClaims(testReaderClaims, h.Create)).Methods("POST")

	body := validBody()
	body["base_sha256"] = pageSHA256("")
	body["proposed_markdown"] = strings.Repeat("м", 2137)

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ожидался 201 для текста обычной длины на пустой основе, получено %d: %s",
			rec.Code, rec.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatal("запись не создана")
	}
}

func TestSuggestionRelativeCapFloorStillRejectsOnEmptyBase(t *testing.T) {
	// Пол не отменяет отказ целиком — он лишь поднимает порог. Текст длиннее
	// пола (20 000) на пустой основе обязан отвергаться, как и раньше.
	store := &fakeSuggestionStore{recentIPs: map[string]int{}}
	pages := pageStoreWith(&models.Page{
		ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: "",
	}, nil)
	h := NewPageSuggestionHandler(store, pages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions",
		withClaims(testReaderClaims, h.Create)).Methods("POST")

	body := validBody()
	body["base_sha256"] = pageSHA256("")
	body["proposed_markdown"] = strings.Repeat("м", suggestionRelativeCapFloor+1)

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ожидался 422 для текста длиннее пола на пустой основе, получено %d: %s",
			rec.Code, rec.Body.String())
	}
	if len(store.created) != 0 {
		t.Fatal("запись создана при превышении пола на пустой основе")
	}
	if !strings.Contains(rec.Body.String(), fmt.Sprint(suggestionRelativeCapFloor)) {
		t.Fatalf("в отказе нет числа пола (%d): %s", suggestionRelativeCapFloor, rec.Body.String())
	}
}

func TestSuggestionRateLimited(t *testing.T) {
	store, _, r := suggestionTestRig(t)
	// clientIP при trustProxy=false берёт RemoteAddr; httptest ставит 192.0.2.1.
	store.recentIPs[hashIP("192.0.2.1", "соль")] = suggestionRatePerHour

	rec := postSuggestion(t, r, validBody())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("ожидался 429, получено %d", rec.Code)
	}
	if len(store.created) != 0 {
		t.Fatal("предел выбран, а запись всё равно создана")
	}
}

func TestSuggestionControlCharsRejectedBeforeAnyDBWork(t *testing.T) {
	store := &fakeSuggestionStore{recentIPs: map[string]int{}}
	var getByIDCalls int
	pages := pageStoreWith(&models.Page{
		ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: testPageText,
	}, &getByIDCalls)
	h := NewPageSuggestionHandler(store, pages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions",
		withClaims(testReaderClaims, h.Create)).Methods("POST")

	body := validBody()
	// Нулевой байт роняет вставку в TEXT, строка не создаётся, предел
	// частоты её не считает — и запрос повторяется без ограничений.
	body["proposed_markdown"] = "Текст полосы\x00после правки"

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ожидался 422, получено %d", rec.Code)
	}
	if len(store.created) != 0 {
		t.Fatal("запись создана при управляющем символе")
	}
	// Ловит именно ту регрессию, от которой тест назван защищать: если отбой
	// по управляющим символам переедет ниже h.pages.GetByID (проверку 409 по
	// base_sha256), страница будет запрошена у хранилища раньше отказа.
	if getByIDCalls != 0 {
		t.Fatalf("страница запрошена у хранилища до отбоя по управляющим символам: %d обращений", getByIDCalls)
	}
}

// TestSuggestionRejectsInvisibleRunes — находка финальной проверки ветки.
// Отбой стоял на unicode.IsControl, то есть только на категории Cc, а
// переключатели направления письма, символы нулевой ширины и метка порядка
// байтов — это Cf, для которой IsControl отвечает false. Правка, отличающаяся
// от основы ТОЛЬКО такими символами, давала редактору дифф с <ins>/<del> над
// визуально одинаковыми словами — а дифф это его единственный инструмент.
// Принятая правка уехала бы в EPUB, FB2 и предметный указатель.
func TestSuggestionRejectsInvisibleRunes(t *testing.T) {
	cases := map[string]string{
		"нулевая ширина U+200B":          "Текст полосы\u200bпосле правки",
		"переключатель LRE U+202A":       "Текст полосы\u202aпосле правки",
		"переключатель RLO U+202E":       "Текст полосы\u202eпосле правки",
		"снятие переключателя U+202C":    "Текст полосы\u202cпосле правки",
		"метка порядка байтов U+FEFF":    "\ufeffТекст полосы после правки",
		"соединитель нулевой ширины ZWJ": "Текст полосы\u200dпосле правки",
		"изолятор направления U+2066":    "Текст полосы\u2066после правки",
		"склейка слов U+2060":            "Текст полосы\u2060после правки",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			store, _, r := suggestionTestRig(t)
			body := validBody()
			body["proposed_markdown"] = text

			rec := postSuggestion(t, r, body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("ожидался 422, получено %d: %s", rec.Code, rec.Body.String())
			}
			if len(store.created) != 0 {
				t.Fatal("запись создана при невидимом символе")
			}
		})
	}
}

// TestSuggestionKeepsSoftHyphen — парное требование к отбою невидимых.
// Мягкий перенос тоже Cf, но замер корпуса (27.08.2026, 45 495 полос) даёт
// 24 полосы с ним при нуле по всем остальным невидимым. Отбить его — значит
// закрыть подачу правки для двух десятков живых полос.
func TestSuggestionKeepsSoftHyphen(t *testing.T) {
	store, _, r := suggestionTestRig(t)

	body := validBody()
	body["proposed_markdown"] = "Текст по\u00adлосы после правки"

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("мягкий перенос обязан проходить, получено %d: %s",
			rec.Code, rec.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatal("запись не создана")
	}
}

func TestSuggestionKeepsNewlinesAndTabs(t *testing.T) {
	store, _, r := suggestionTestRig(t)

	body := validBody()
	// Текст полосы многострочный: отбой не должен задевать разметку.
	body["proposed_markdown"] = "Первая строка\nВторая\tс табуляцией\r\nТретья"

	rec := postSuggestion(t, r, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("перенос строки и табуляция обязаны проходить, получено %d: %s",
			rec.Code, rec.Body.String())
	}
	if len(store.created) != 1 {
		t.Fatal("запись не создана")
	}
}

// TestSuggestionCreateRequiresLogin — наследник TestSuggestionBadReaderKeyRejected:
// раньше проверка ловила ключ негодного формата, теперь ключа нет вовсе,
// автора Create берёт из claims. Сторожит ту же страховочную ветку, что и
// TestMineRequiresLogin: маршрут стоит на подроутере reader и claims должны
// быть в контексте всегда, но если однажды это перестанет быть так — 401, а
// не паника на разыменовании nil.
func TestSuggestionCreateRequiresLogin(t *testing.T) {
	suggestions := &fakeSuggestionStore{recentIPs: map[string]int{}}
	pages := pageStoreWith(&models.Page{
		ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: testPageText,
	}, nil)
	h := NewPageSuggestionHandler(suggestions, pages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)

	r := mux.NewRouter()
	// Без withClaims — запрос идёт так, как если бы дошёл до Create в обход
	// AuthMiddleware.
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions", h.Create).Methods("POST")

	rec := postSuggestion(t, r, validBody())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался 401 без входа, получено %d", rec.Code)
	}
	if len(suggestions.created) != 0 {
		t.Fatal("без claims правка не должна была записаться")
	}
}

func TestSuggestionUsesProxyHeaderOnlyWhenTrusted(t *testing.T) {
	// clientIP и hashIP приходят готовыми из ветки подвала и протестированы
	// там. Здесь проверяется своё: что обработчик передаёт им флаг доверия, а
	// не забыл его. Ошибка проводки тихая — предел частоты просто перестанет
	// различать читателей, и заметить это можно будет только по заливу.
	page := &models.Page{ID: 42, WorkID: 7, PageNumber: 3, ContentMarkdown: testPageText}

	for _, tc := range []struct {
		name       string
		trustProxy bool
		wantIP     string
	}{
		{"без доверия берётся RemoteAddr", false, "192.0.2.1"},
		{"с доверием берётся заголовок", true, "203.0.113.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeSuggestionStore{recentIPs: map[string]int{}}
			h := NewPageSuggestionHandler(store, pageStoreWith(page, nil), &fakePageVersions{},
				&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", tc.trustProxy)

			r := mux.NewRouter()
			r.HandleFunc("/api/works/{workId}/pages/{pageId}/suggestions",
				withClaims(testReaderClaims, h.Create)).Methods("POST")

			raw, _ := json.Marshal(validBody())
			req := httptest.NewRequest(http.MethodPost,
				"/api/works/7/pages/42/suggestions", bytes.NewReader(raw))
			req.Header.Set("X-Real-IP", "203.0.113.9")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated {
				t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
			}
			if got, want := store.created[0].IPHash, hashIP(tc.wantIP, "соль"); got != want {
				t.Fatalf("отметка адреса посчитана не от того адреса: %q, ожидалось от %s",
					got, tc.wantIP)
			}
		})
	}
}

func TestSuggestionIPHashNeverLeaves(t *testing.T) {
	store, _, r := suggestionTestRig(t)

	rec := postSuggestion(t, r, validBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "ip_hash") {
		t.Fatalf("отметка адреса просочилась в ответ: %s", rec.Body.String())
	}
	if store.created[0].IPHash == "" {
		t.Fatal("отметка адреса не записана — предел частоты работать не будет")
	}
}

// mineRig собирает /api/suggestions/mine. claims == nil эмулирует запрос,
// дошедший до Mine без прохода через AuthMiddleware (страховочная ветка).
func mineRig(t *testing.T, claims *auth.Claims, rows []repository.SuggestionRow) (*fakeSuggestionStore, http.Handler) {
	t.Helper()
	store := &fakeSuggestionStore{recentIPs: map[string]int{}, rows: rows}
	h := NewPageSuggestionHandler(store, &fakePageStore{}, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/suggestions/mine", withClaims(claims, h.Mine)).Methods("GET")
	return store, r
}

// TestMineRequiresLogin — наследник TestMineRequiresReaderKeyHeader: раньше
// без заголовка X-Reader-Key маршрут отвечал 400, теперь заголовка нет вовсе,
// личность читателя даёт вход. Без claims в контексте (маршрут стоит на
// reader — до сюда в бою не дойти) — 401, та же страховка, что в Create.
func TestMineRequiresLogin(t *testing.T) {
	_, r := mineRig(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/mine", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался 401 без входа, получено %d", rec.Code)
	}
}

func TestMineReturnsEmptyArrayNotNull(t *testing.T) {
	_, r := mineRig(t, testReaderClaims, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/mine", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	// Известный дефект платформы: списочные маршруты по умолчанию отдают null
	// вместо []. У читателя без единой правки список обязан быть [].
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("ожидался [], получено %q", got)
	}
}

// TestMineIgnoresStrayReaderKeyHeader — наследник TestMineTakesKeyFromHeaderNotQuery:
// раньше маршрут не пускал ключ из query (только из заголовка), теперь ключа
// нет вовсе — читателя устанавливает вход. Если на клиенте по инерции
// остался заголовок X-Reader-Key от старого кода, он обязан молча
// игнорироваться, а не подменять автора.
func TestMineIgnoresStrayReaderKeyHeader(t *testing.T) {
	store, r := mineRig(t, testReaderClaims, []repository.SuggestionRow{{ID: 1, PageNumber: 3}})

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/mine", nil)
	req.Header.Set("X-Reader-Key", "11111111-1111-4111-8111-111111111111")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if store.calledUserID != testReaderClaims.UserID {
		t.Fatalf("ListByUserID вызван с userID=%d, ожидался %d из claims — заголовок не должен был победить",
			store.calledUserID, testReaderClaims.UserID)
	}
}

// TestMineListsByUser — Step 1 брифа: Mine отдаёт правки текущего читателя,
// беря его из claims, а не из какого-либо предъявительского секрета.
func TestMineListsByUser(t *testing.T) {
	want := []repository.SuggestionRow{{ID: 7, PageNumber: 3}}
	store, r := mineRig(t, testReaderClaims, want)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/mine", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if store.calledUserID != testReaderClaims.UserID {
		t.Fatalf("ListByUserID вызван с userID=%d, ожидался %d", store.calledUserID, testReaderClaims.UserID)
	}
	var got []repository.SuggestionRow
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("ответ = %+v, ожидалась строка с id=7", got)
	}
}

func queueRig(t *testing.T, store *fakeSuggestionStore) http.Handler {
	t.Helper()
	h := NewPageSuggestionHandler(store, &fakePageStore{}, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/suggestions", h.List).Methods("GET")
	r.HandleFunc("/api/suggestions/{id}", h.Get).Methods("GET")
	return r
}

func TestQueueListReturnsItemsAndTotal(t *testing.T) {
	store := &fakeSuggestionStore{
		rows:  []repository.SuggestionRow{{ID: 3, PageNumber: 12, Stale: true, LengthDelta: -4}},
		total: 17,
	}
	r := queueRig(t, store)

	// status, limit и offset все три заданы явно и все три отличны от
	// умолчаний обработчика — иначе тест не отличил бы «передал то, что
	// пришло» от «тихо подставил свои умолчания».
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions?status=новое&limit=1&offset=5", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	var payload struct {
		Items []repository.SuggestionRow `json:"items"`
		Total int                        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if payload.Total != 17 || len(payload.Items) != 1 {
		t.Fatalf("ожидались total=17 и одна строка, получено %+v", payload)
	}
	if !payload.Items[0].Stale {
		t.Fatal("признак устаревания не доехал до клиента")
	}

	// Склад обязан получить именно то, что пришло в запросе, а не свои
	// умолчания: fakeSuggestionStore.List молча возвращал бы то же самое
	// payload'ом выше, даже если бы обработчик отбросил все три параметра.
	if !store.listCalled {
		t.Fatal("склад ни разу не вызван")
	}
	if store.listStatus == nil || *store.listStatus != models.SuggestionNew {
		t.Fatalf("статус фильтра не доехал до склада: %v", store.listStatus)
	}
	if store.listLimit != 1 {
		t.Fatalf("limit не доехал до склада: получено %d, ожидалось 1", store.listLimit)
	}
	if store.listOffset != 5 {
		t.Fatalf("offset не доехал до склада: получено %d, ожидалось 5", store.listOffset)
	}
}

// TestSuggestionQueueShowsAuthorNickname — Step 1 брифа: SuggestionRow несёт
// ник автора (LEFT JOIN users в репозитории), и List отдаёт его наружу как
// есть, ничего не подставляя и не пряча.
func TestSuggestionQueueShowsAuthorNickname(t *testing.T) {
	nickname := "Читатель Пётр"
	store := &fakeSuggestionStore{
		rows:  []repository.SuggestionRow{{ID: 9, PageNumber: 1, AuthorNickname: &nickname}},
		total: 1,
	}
	r := queueRig(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"author_nickname":"Читатель Пётр"`) {
		t.Fatalf("ответ не несёт ник автора: %s", rec.Body.String())
	}
}

func TestQueueListRejectsUnknownStatus(t *testing.T) {
	store := &fakeSuggestionStore{}
	r := queueRig(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions?status=накопитель", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400 на неизвестный статус, получено %d", rec.Code)
	}
	if store.listCalled {
		t.Fatal("склад не должен вызываться при отвергнутом статусе")
	}
}

func TestQueueListRejectsOutOfRangeLimit(t *testing.T) {
	// Проверка n<0 || n>200 в коде осталась бы недоказанной: реализация,
	// пропускающая любое из этих значений прямо в SQL, прошла бы остальные
	// тесты файла без единого отличия в ответе.
	for _, raw := range []string{"-1", "201", "99999"} {
		t.Run("limit="+raw, func(t *testing.T) {
			store := &fakeSuggestionStore{}
			r := queueRig(t, store)

			req := httptest.NewRequest(http.MethodGet, "/api/suggestions?limit="+raw, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("ожидался 400 на limit=%s, получено %d", raw, rec.Code)
			}
			if store.listCalled {
				t.Fatalf("склад не должен вызываться при отвергнутом limit=%s", raw)
			}
		})
	}
}

func TestQueueListRejectsNegativeOffset(t *testing.T) {
	store := &fakeSuggestionStore{}
	r := queueRig(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions?offset=-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400 на offset=-1, получено %d", rec.Code)
	}
	if store.listCalled {
		t.Fatal("склад не должен вызываться при отвергнутом offset")
	}
}

func TestQueueListLimitZeroReturnsCountOnly(t *testing.T) {
	// limit=0 — путь значка новых предложений в шапке: только total, без
	// строк. На пустой очереди (total=0) этот путь неотличим от умолчания в
	// 50 — оба дают пустой список. Здесь total нарочно ненулевой при пустых
	// items, чтобы отличить «limit=0 передан складу» от «сработало умолчание
	// и склад просто ничего не нашёл».
	store := &fakeSuggestionStore{
		rows:  []repository.SuggestionRow{},
		total: 17,
	}
	r := queueRig(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions?limit=0", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	var payload struct {
		Items []repository.SuggestionRow `json:"items"`
		Total int                        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if payload.Total != 17 {
		t.Fatalf("значку в шапке нужен total=17 при пустых items, получено %d", payload.Total)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("limit=0 не должен возвращать строки, получено %d", len(payload.Items))
	}
	if store.listLimit != 0 {
		t.Fatalf("обработчик обязан передать limit=0 складу как есть, получено %d", store.listLimit)
	}
}

func TestQueueDetailCarriesThreeTexts(t *testing.T) {
	store := &fakeSuggestionStore{detail: &repository.SuggestionDetail{
		SuggestionRow:    repository.SuggestionRow{ID: 3, PageNumber: 12, Stale: true},
		BaseMarkdown:     "основа",
		ProposedMarkdown: "предложено",
		CurrentMarkdown:  "текущий",
	}}
	r := queueRig(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/suggestions/3", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	var d repository.SuggestionDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if d.BaseMarkdown == "" || d.ProposedMarkdown == "" || d.CurrentMarkdown == "" {
		t.Fatalf("экрану модерации нужны все три текста, получено %+v", d)
	}
}

func decisionRig(t *testing.T, store *fakeSuggestionStore, pages *fakePageStore) http.Handler {
	t.Helper()
	h := NewPageSuggestionHandler(store, pages, &fakePageVersions{}, &fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	r := mux.NewRouter()
	post := func(path string, fn http.HandlerFunc) {
		r.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
			// Решение принимает редактор: подкладываем его в контекст, как это
			// делает AuthMiddleware на живом маршруте.
			fn(w, req.WithContext(context.WithValue(req.Context(),
				middleware.UserContextKey, &auth.Claims{UserID: 9, Role: models.RoleEditor})))
		}).Methods("POST")
	}
	post("/api/suggestions/{id}/accept", h.Accept)
	post("/api/suggestions/{id}/reject", h.Reject)
	return r
}

func TestAcceptWritesEditorTextNotProposed(t *testing.T) {
	page := &models.Page{ID: 42, WorkID: 7, PageNumber: 3,
		ContentMarkdown: "текущий текст", Status: models.PageStatusMachineProofread}
	pages := pageStoreWith(page, nil)
	store := &fakeSuggestionStore{detail: &repository.SuggestionDetail{
		SuggestionRow:    repository.SuggestionRow{ID: 3, PageID: 42},
		BaseMarkdown:     "основа",
		ProposedMarkdown: "текст читателя",
		CurrentMarkdown:  "текущий текст",
	}}
	r := decisionRig(t, store, pages)

	body, _ := json.Marshal(map[string]any{
		"content_markdown": "итоговый текст редактора",
		"comment":          "принято предложение #3",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/3/accept", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if page.ContentMarkdown != "итоговый текст редактора" {
		t.Fatalf("в полосу лёг %q, а должен был текст редактора", page.ContentMarkdown)
	}
	if len(pages.savedVersions) != 1 || pages.savedVersions[0].UserID != 9 {
		t.Fatalf("версия от имени модератора не создана: %+v", pages.savedVersions)
	}
	if store.resolvedStatus != models.SuggestionAccepted {
		t.Fatalf("статус предложения %q, ожидался %q",
			store.resolvedStatus, models.SuggestionAccepted)
	}
	// Автор версии (проверен выше) и модератор, решивший предложение,
	// обязаны быть одним и тем же редактором.
	if store.resolvedModeratorID != 9 {
		t.Fatalf("модератор предложения %d, ожидался 9", store.resolvedModeratorID)
	}
}

// TestAcceptReanchorsDocumentCuts — вторая половина сторожа единства пути
// (I6, найдено обзором ветки): у RestoreVersion уже был тест на обе машины,
// держащиеся за текст полосы, а у принятия читательского предложения
// утверждения про переякоривание вклеек не было вовсе — decisionRig везде
// подставляет пустой &fakeDocumentCutStore{}, который нечего проверять.
// Здесь хранилище вклеек заводится с фикстурой отдельно от общего rig'а,
// чтобы не размножать пустой параметр по всем остальным тестам этого файла.
func TestAcceptReanchorsDocumentCuts(t *testing.T) {
	page := &models.Page{ID: 42, WorkID: 7, PageNumber: 3,
		ContentMarkdown: "текущий текст", Status: models.PageStatusMachineProofread}
	pages := pageStoreWith(page, nil)
	store := &fakeSuggestionStore{detail: &repository.SuggestionDetail{
		SuggestionRow:    repository.SuggestionRow{ID: 3, PageID: 42},
		BaseMarkdown:     "основа",
		ProposedMarkdown: "текст читателя",
		CurrentMarkdown:  "текущий текст",
	}}
	// Голова и хвост цитаты — первое и последнее слово итогового текста
	// редактора, которым завершится Accept; оба должны найтись заново.
	cuts := newFakeDocumentCutStore(&models.DocumentCut{
		ID: 5, DocumentID: 11,
		Anchor: models.Anchor{
			StartPageID: 42, StartOffset: 0, EndPageID: 42,
			EndOffset: len("текущий"),
			HeadQuote: "итоговый", TailQuote: "редактора",
		},
		Status: models.CutStatusOK,
	})
	h := NewPageSuggestionHandler(store, pages, &fakePageVersions{}, &fakeFragmentStore{}, cuts, "соль", false)
	r := mux.NewRouter()
	r.HandleFunc("/api/suggestions/{id}/accept", func(w http.ResponseWriter, req *http.Request) {
		h.Accept(w, req.WithContext(context.WithValue(req.Context(),
			middleware.UserContextKey, &auth.Claims{UserID: 9, Role: models.RoleEditor})))
	}).Methods("POST")

	body, _ := json.Marshal(map[string]any{
		"content_markdown": "итоговый текст редактора",
		"comment":          "принято предложение #3",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/3/accept", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}

	edited := "итоговый текст редактора"
	start, end, ok := cuts.movedTo(5)
	if !ok {
		t.Fatal("вклейка разбора не переякорена: принятие предложения не идёт через applyPageEdit целиком")
	}
	if got := edited[start:end]; got != edited {
		t.Fatalf("вклейка переякорена на %q, ожидалось %q", got, edited)
	}
}

func TestAcceptKeepsPageStatus(t *testing.T) {
	// Статус нарочно не «вычитано_машиной» — та же константа, которой
	// объясняется требование, фигурирует и в TestAcceptWritesEditorTextNotProposed.
	// Реализация, жёстко вписавшая PageStatusMachineProofread вместо передачи
	// page.Status, прошла бы оба теста; другой статус здесь это ловит.
	page := &models.Page{ID: 42, ContentMarkdown: "текущий",
		Status: models.PageStatusHasIssues}
	pages := pageStoreWith(page, nil)
	store := &fakeSuggestionStore{detail: &repository.SuggestionDetail{
		SuggestionRow: repository.SuggestionRow{ID: 3, PageID: 42},
		BaseMarkdown:  "основа", ProposedMarkdown: "правка", CurrentMarkdown: "текущий",
	}}
	r := decisionRig(t, store, pages)

	body, _ := json.Marshal(map[string]any{"content_markdown": "итог", "comment": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/3/accept", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	// Читатель судит о тексте, а не о том, вычитана ли полоса. Молча сбить
	// статус в значение по умолчанию или в константу из объяснений было бы
	// хуже, чем не дать редактору сменить статус на этом экране.
	if page.Status != models.PageStatusHasIssues {
		t.Fatalf("статус полосы изменился на %q", page.Status)
	}
}

func TestAcceptTwiceConflicts(t *testing.T) {
	page := &models.Page{ID: 42, ContentMarkdown: "текущий"}
	pages := pageStoreWith(page, nil)
	store := &fakeSuggestionStore{
		detail: &repository.SuggestionDetail{
			SuggestionRow: repository.SuggestionRow{ID: 3, PageID: 42},
			BaseMarkdown:  "основа", ProposedMarkdown: "правка", CurrentMarkdown: "текущий",
		},
		resolveErr: repository.ErrSuggestionResolved,
	}
	r := decisionRig(t, store, pages)

	body, _ := json.Marshal(map[string]any{"content_markdown": "итог", "comment": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/3/accept", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("ожидался 409 на повторное решение, получено %d", rec.Code)
	}
	// Отметка о разборе обязана стоять до applyPageEdit, а не после: иначе
	// два модератора, решающих одно и то же предложение, могли бы оба
	// применить к полосе разные итоговые тексты — второй молча побеждает,
	// а ответ 409 всё равно приходит (Resolve зовётся и в переставленном
	// порядке, просто вторым). Только неизменившийся текст полосы отличает
	// правильный порядок от переставленного.
	if page.ContentMarkdown != "текущий" {
		t.Fatalf("текст полосы изменился при отказе в разборе: %q", page.ContentMarkdown)
	}
	if len(pages.savedVersions) != 0 {
		t.Fatalf("версия создана при отказе в разборе: %+v", pages.savedVersions)
	}
}

func TestRejectRequiresReasonFromList(t *testing.T) {
	store := &fakeSuggestionStore{detail: &repository.SuggestionDetail{
		SuggestionRow: repository.SuggestionRow{ID: 3, PageID: 42},
	}}
	r := decisionRig(t, store, &fakePageStore{})

	body, _ := json.Marshal(map[string]any{"reason": "просто не нравится"})
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/3/reject", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ожидался 422 на причину вне списка, получено %d", rec.Code)
	}
}

func TestRejectStoresReason(t *testing.T) {
	store := &fakeSuggestionStore{detail: &repository.SuggestionDetail{
		SuggestionRow: repository.SuggestionRow{ID: 3, PageID: 42},
	}}
	r := decisionRig(t, store, &fakePageStore{})

	body, _ := json.Marshal(map[string]any{"reason": string(models.RejectAsInOriginal)})
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/3/reject", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, тело %s", rec.Code, rec.Body.String())
	}
	if store.resolvedReason == nil || *store.resolvedReason != models.RejectAsInOriginal {
		t.Fatalf("причина не сохранена: %v", store.resolvedReason)
	}
}
