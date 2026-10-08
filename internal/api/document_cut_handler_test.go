package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// newTestCutHandler строит DocumentCutHandler поверх doc и одной живой
// вклейки (id=1, work_id=1, страница 1) — достаточно, чтобы Full и Create
// дошли до проверки, которую тестирует вызывающий тест, а не упали раньше на
// пустом хранилище.
// newTestCutHandler строит обработчик вокруг разбора doc и ОДНОЙ живой
// вклейки id=1. Если у doc пустое тело, оно заполняется плейсхолдером этой
// вклейки: догрузка отдаёт только ту вклейку, которую называет видимая
// пришедшему редакция (I3), и разбор без единого тега не назвал бы ни одной.
func newTestCutHandler(t *testing.T, doc *models.Document) *DocumentCutHandler {
	t.Helper()
	if doc.MarkdownContent == "" {
		doc.MarkdownContent = `<cut id="1">`
	}
	if doc.PublishedMarkdown == "" {
		doc.PublishedMarkdown = doc.MarkdownContent
	}
	page := &models.Page{ID: 501, WorkID: 1, PageNumber: 1, ContentMarkdown: "живая полоса вклейки"}
	cut := &models.DocumentCut{
		ID: 1, DocumentID: doc.ID, WorkID: ptrInt64(1),
		Anchor: models.Anchor{
			StartPageID: page.ID, StartOffset: 0,
			EndPageID: page.ID, EndOffset: len(page.ContentMarkdown),
		},
		Status: models.CutStatusOK, SourceTitle: "источник",
	}
	return NewDocumentCutHandler(
		&fakeDocumentStore{doc: doc},
		&fakeCutFullStore{cut: cut, pages: []*models.Page{page}},
		&fakeWorkStore{},
		newFakePageStoreWithPages(page),
		markdown.NewRenderer(),
	)
}

// newFakePageStoreWithPages — подставной PageStore для тестов Create: ищет
// по (work_id, page_number), как это делает GetByWorkAndPageNumber настоящего
// репозитория. Отсутствие — ошибка, а не (nil, nil) — тот же приём, что и у
// getByWorkAndPageNumberFn по умолчанию в stores_test.go, здесь просто
// заведён поверх нескольких страниц сразу, а не одной функцией на тест.
func newFakePageStoreWithPages(pages ...*models.Page) *fakePageStore {
	return &fakePageStore{
		getByWorkAndPageNumberFn: func(ctx context.Context, workID int64, pageNumber int) (*models.Page, error) {
			for _, p := range pages {
				if p.WorkID == workID && p.PageNumber == pageNumber {
					return p, nil
				}
			}
			return nil, errors.New("page not found")
		},
	}
}

// fakeCutFullStore — подставное хранилище вклеек для маршрута догрузки.
// ByIDs повторяет контракт настоящего репозитория (document_cut_repository.go):
// отдаёт вклейку, только если documentID совпадает с тем, за которым она
// числится, — тем самым тест на «чужой разбор» проверяет ровно то же
// условие, что и SQL (`WHERE document_id = $1 AND id = ANY($2)`), без базы.
type fakeCutFullStore struct {
	cut      *models.DocumentCut
	pages    []*models.Page
	cutsErr  error
	pagesErr error
}

func (s *fakeCutFullStore) ByDocument(ctx context.Context, documentID int64) ([]*models.DocumentCut, error) {
	return nil, nil
}

func (s *fakeCutFullStore) ByIDs(ctx context.Context, documentID int64, ids []int64) ([]*models.DocumentCut, error) {
	if s.cutsErr != nil {
		return nil, s.cutsErr
	}
	if s.cut == nil || s.cut.DocumentID != documentID {
		return nil, nil
	}
	for _, id := range ids {
		if id == s.cut.ID {
			return []*models.DocumentCut{s.cut}, nil
		}
	}
	return nil, nil
}

func (s *fakeCutFullStore) ByPage(ctx context.Context, pageID int64) ([]*models.DocumentCut, error) {
	return nil, nil
}

func (s *fakeCutFullStore) Create(ctx context.Context, cut *models.DocumentCut) error { return nil }

func (s *fakeCutFullStore) DeleteUnreferenced(ctx context.Context, documentID int64, keep []int64) error {
	return nil
}

func (s *fakeCutFullStore) UpdateAnchor(ctx context.Context, id int64, start, end int, startHash, endHash string) error {
	return nil
}

func (s *fakeCutFullStore) SetStatus(ctx context.Context, id int64, status string) error {
	return nil
}

func (s *fakeCutFullStore) PagesOfCut(ctx context.Context, cut *models.DocumentCut) ([]*models.Page, error) {
	if s.pagesErr != nil {
		return nil, s.pagesErr
	}
	return s.pages, nil
}

// longCutFixture — та же вклейка (по форме), что в TestRenderCutTrimsLongCut:
// заведомо длиннее cutTrimChars, две полосы.
func longCutFixture(documentID int64) (*models.DocumentCut, []*models.Page) {
	long := strings.Repeat("Очень длинный абзац корпуса. ", 700) // 20 300 знаков, заведомо больше порога 14000
	cut := &models.DocumentCut{
		ID: 9, DocumentID: documentID, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 11, EndOffset: 10},
		Status: models.CutStatusOK, SourceTitle: "источник",
	}
	pages := []*models.Page{cutPage(10, 4, long), cutPage(11, 5, long)}
	return cut, pages
}

// newCutFullRequest строит запрос догрузки с заданными claims в контексте —
// Full теперь читает разбор и проверяет его видимость (задача 7), и тесты,
// которым доступ к разбору не важен по существу, дают его editorClaims(), а
// не остаются анонимными по умолчанию.
//
// slug адресует разбор так же, как documentKey (document_key.go) читает его
// из пути: короткий сотруднический адрес — slug без ника. Здесь у всех
// вызывающих тестов разбор сотруднический (AuthorNickname == ""), поэтому
// "nickname" в vars не заводится вовсе.
func newCutFullRequest(slug, cutID string, claims *auth.Claims) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/documents/"+slug+"/cuts/"+cutID, nil)
	req = mux.SetURLVars(req, map[string]string{"slug": slug, "cutId": cutID})
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, claims))
	}
	return req
}

// Вклейка своего разбора отдаётся целиком: длиннее подрезанной версии того же
// среза (renderCutTrimmed её бы оборвала — cutTrimChars меньше длины фикстуры)
// и не несёт пометки document-cut--trimmed.
func TestDocumentCutHandlerFullReturnsWholeCutForOwnDocument(t *testing.T) {
	cut, pages := longCutFixture(7)
	handler := NewDocumentCutHandler(
		&fakeDocumentStore{doc: &models.Document{ID: 7, Slug: "razbor-7", MarkdownContent: `<cut id="9">`}},
		&fakeCutFullStore{cut: cut, pages: pages},
		&fakeWorkStore{},
		&fakePageStore{},
		markdown.NewRenderer(),
	)

	rec := httptest.NewRecorder()
	handler.Full(rec, newCutFullRequest("razbor-7", "9", editorClaims()))

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d; тело: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}

	trimmed := renderCutTrimmed(markdown.NewRenderer(), cut, pages, 1, 0)
	if len(got.HTML) <= len(trimmed) {
		t.Fatalf("догрузка не длиннее подрезанной версии: %d против %d", len(got.HTML), len(trimmed))
	}
	if strings.Contains(got.HTML, "document-cut--trimmed") {
		t.Fatalf("догрузка помечена как подрезанная: %s", got.HTML[:200])
	}
}

// Вклейка чужого разбора не отдаётся: ByIDs сверяет document_id так же, как
// настоящий репозиторий, и путь с ЧУЖИМ id разбора должен получить 410 в
// форме {"message": …}, а не text/plain и не 404 (404 nginx фронта подменяет
// оболочкой SPA кодом 200 — крышка «этого больше нет» до клиента не доедет).
func TestDocumentCutHandlerFullForeignDocumentIsGone(t *testing.T) {
	cut, pages := longCutFixture(7)
	handler := NewDocumentCutHandler(
		&fakeDocumentStore{doc: &models.Document{ID: 99, Slug: "razbor-99", MarkdownContent: `<cut id="9">`}},
		&fakeCutFullStore{cut: cut, pages: pages},
		&fakeWorkStore{},
		&fakePageStore{},
		markdown.NewRenderer(),
	)

	rec := httptest.NewRecorder()
	// Вклейка числится за разбором 7, запрос идёт по разбору 99.
	handler.Full(rec, newCutFullRequest("razbor-99", "9", editorClaims()))

	assertGoneWithMessage(t, rec)
}

// Неизвестный id вклейки отвечает так же, как чужой разбор: 410, JSON.
func TestDocumentCutHandlerFullUnknownCutIsGone(t *testing.T) {
	handler := NewDocumentCutHandler(
		&fakeDocumentStore{},
		&fakeCutFullStore{},
		&fakeWorkStore{},
		&fakePageStore{},
		markdown.NewRenderer(),
	)

	rec := httptest.NewRecorder()
	handler.Full(rec, newCutFullRequest("razbor-7", "404", nil))

	assertGoneWithMessage(t, rec)
}

// Отвязавшаяся (stale) вклейка тоже отвечает 410: Broken() для неё не
// срабатывает (полосы ещё известны), но смещения уже не совпадают с текущим
// текстом полосы — renderCut режет по ним как есть (задача 6, беззащитна
// намеренно), и без этой проверки Full показал бы чужой кусок текста молча.
func TestDocumentCutHandlerFullStaleCutIsGone(t *testing.T) {
	cut, pages := longCutFixture(7)
	cut.Status = models.CutStatusStale
	handler := NewDocumentCutHandler(
		&fakeDocumentStore{doc: &models.Document{ID: 7, Slug: "razbor-7", MarkdownContent: `<cut id="9">`}},
		&fakeCutFullStore{cut: cut, pages: pages},
		&fakeWorkStore{},
		&fakePageStore{},
		markdown.NewRenderer(),
	)

	rec := httptest.NewRecorder()
	handler.Full(rec, newCutFullRequest("razbor-7", "9", editorClaims()))

	assertGoneWithMessage(t, rec)
}

// Сбой хранилища у ByIDs — не то же самое, что честное отсутствие вклейки:
// клиент, различающий 410 (прячет кнопку «развернуть», не повторяет запрос) и
// 500 (временная неполадка), должен получить 500, а не решить, что вклейку
// сняли навсегда.
func TestDocumentCutHandlerFullByIDsStorageErrorIsServerError(t *testing.T) {
	handler := NewDocumentCutHandler(
		&fakeDocumentStore{doc: &models.Document{ID: 7, Slug: "razbor-7", MarkdownContent: `<cut id="9">`}},
		&fakeCutFullStore{cutsErr: errors.New("connection reset")},
		&fakeWorkStore{},
		&fakePageStore{},
		markdown.NewRenderer(),
	)

	rec := httptest.NewRecorder()
	handler.Full(rec, newCutFullRequest("razbor-7", "9", editorClaims()))

	assertServerErrorWithMessage(t, rec)
}

// Тот же довод, что выше, для второго обращения к хранилищу в Full —
// PagesOfCut: полосы вклейки не читаются из-за сбоя, а не потому что их снесли.
func TestDocumentCutHandlerFullPagesOfCutStorageErrorIsServerError(t *testing.T) {
	cut, _ := longCutFixture(7)
	handler := NewDocumentCutHandler(
		&fakeDocumentStore{doc: &models.Document{ID: 7, Slug: "razbor-7", MarkdownContent: `<cut id="9">`}},
		&fakeCutFullStore{cut: cut, pagesErr: errors.New("connection reset")},
		&fakeWorkStore{},
		&fakePageStore{},
		markdown.NewRenderer(),
	)

	rec := httptest.NewRecorder()
	handler.Full(rec, newCutFullRequest("razbor-7", "9", editorClaims()))

	assertServerErrorWithMessage(t, rec)
}

func assertServerErrorWithMessage(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Status = %d, want %d; тело: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json (не text/plain — фронт его не разбирает)", ct)
	}
	var got struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело ошибки не JSON: %v (%s)", err, rec.Body.String())
	}
	if got.Message == "" {
		t.Fatalf("тело ошибки без message: %s", rec.Body.String())
	}
}

func assertGoneWithMessage(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusGone {
		t.Fatalf("Status = %d, want %d; тело: %s", rec.Code, http.StatusGone, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json (не text/plain — фронт его не разбирает)", ct)
	}
	var got struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело ошибки не JSON: %v (%s)", err, rec.Body.String())
	}
	if got.Message == "" {
		t.Fatalf("тело ошибки без message: %s", rec.Body.String())
	}
}

// Создание вклейки берёт якорные цитаты из ТЕКУЩЕГО текста полос, а не с
// клиента: клиент присылает границы, а чем вклейка держится — дело сервера.
func TestCreateCutTakesQuotesFromPageText(t *testing.T) {
	const text = "начало полосы. Ленин писал так. конец полосы."
	pages := newFakePageStoreWithPages(&models.Page{
		ID: 10, WorkID: 1, PageNumber: 4, ContentMarkdown: text,
	})
	cuts := newFakeDocumentCutStore()
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		pages:     pages,
		cuts:      cuts,
		renderer:  markdown.NewRenderer(),
	}

	start := strings.Index(text, "Ленин")
	end := start + len("Ленин писал так.")
	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": start,
		"end_page": 4, "end_offset": end,
		"source_title": "Ленин. Что делать? // ПСС, т. 6, с. 4.",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	saved := cuts.lastCreated()
	if !strings.HasPrefix(saved.HeadQuote, "Ленин") {
		t.Fatalf("головная цитата взята не из текста полосы: %q", saved.HeadQuote)
	}
	if !strings.HasSuffix(saved.TailQuote, "так.") {
		t.Fatalf("хвостовая цитата взята не из текста полосы: %q", saved.TailQuote)
	}
	if saved.StartHash == "" || saved.EndHash == "" {
		t.Fatal("хэши полос не записаны — переякоривание не поймёт, изменилась ли полоса")
	}
	if saved.Status != models.CutStatusOK {
		t.Fatalf("состояние новой вклейки %q, ожидалось %q", saved.Status, models.CutStatusOK)
	}
}

// Границы вне длины полосы — 400, а не паника и не молчаливый пустой срез.
func TestCreateCutRejectsOffsetsOutsidePage(t *testing.T) {
	pages := newFakePageStoreWithPages(&models.Page{
		ID: 10, WorkID: 1, PageNumber: 4, ContentMarkdown: "короткая полоса",
	})
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		pages:     pages,
		cuts:      newFakeDocumentCutStore(),
		renderer:  markdown.NewRenderer(),
	}

	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": 0,
		"end_page": 4, "end_offset": 99999, "source_title": "источник",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", rec.Code)
	}
}

// Разбор, которого действительно нет, отвечает 404 — тот же приём
// (storageNotFound), что уже разводит эти два исхода в Full/Delete.
func TestCreateCutUnknownDocumentIsNotFound(t *testing.T) {
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{},
		pages:     newFakePageStoreWithPages(),
		cuts:      newFakeDocumentCutStore(),
		renderer:  markdown.NewRenderer(),
	}

	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": 0,
		"end_page": 4, "end_offset": 1, "source_title": "источник",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404: %s", rec.Code, rec.Body.String())
	}
}

// Сбой хранилища при проверке разбора — не то же самое, что честное
// отсутствие: клиент, различающий 404 («такого разбора нет») и 500
// («попробуйте ещё раз»), обязан получить второе.
func TestCreateCutDocumentStorageErrorIsServerError(t *testing.T) {
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{getErr: errors.New("connection reset")},
		pages:     newFakePageStoreWithPages(),
		cuts:      newFakeDocumentCutStore(),
		renderer:  markdown.NewRenderer(),
	}

	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": 0,
		"end_page": 4, "end_offset": 1, "source_title": "источник",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assertServerErrorWithMessage(t, rec)
}

// Несуществующая начальная полоса — честный 400 (неверный номер с клиента),
// не 500: fakePageStore{} без getByWorkAndPageNumberFn отдаёт "page N of
// work M not found" тем же приёмом, что и настоящий репозиторий.
func TestCreateCutUnknownStartPageIsBadRequest(t *testing.T) {
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		pages:     &fakePageStore{},
		cuts:      newFakeDocumentCutStore(),
		renderer:  markdown.NewRenderer(),
	}

	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": 0,
		"end_page": 4, "end_offset": 1, "source_title": "источник",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400: %s", rec.Code, rec.Body.String())
	}
}

// Сбой хранилища при чтении начальной полосы — 500, тем же доводом, что и у
// разбора: причина не «неверный номер», а «база сейчас недоступна».
func TestCreateCutStartPageStorageErrorIsServerError(t *testing.T) {
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		pages: &fakePageStore{
			getByWorkAndPageNumberFn: func(ctx context.Context, workID int64, pageNumber int) (*models.Page, error) {
				return nil, errors.New("connection reset")
			},
		},
		cuts:     newFakeDocumentCutStore(),
		renderer: markdown.NewRenderer(),
	}

	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": 0,
		"end_page": 4, "end_offset": 1, "source_title": "источник",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assertServerErrorWithMessage(t, rec)
}

// Тот же довод для конечной полосы — отдельная ветка кода: Create читает её
// вторым вызовом, когда конец не совпадает с началом.
func TestCreateCutEndPageStorageErrorIsServerError(t *testing.T) {
	startPage := &models.Page{ID: 10, WorkID: 1, PageNumber: 4, ContentMarkdown: "текст"}
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		pages: &fakePageStore{
			getByWorkAndPageNumberFn: func(ctx context.Context, workID int64, pageNumber int) (*models.Page, error) {
				if pageNumber == 4 {
					return startPage, nil
				}
				return nil, errors.New("connection reset")
			},
		},
		cuts:     newFakeDocumentCutStore(),
		renderer: markdown.NewRenderer(),
	}

	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": 0,
		"end_page": 5, "end_offset": 1, "source_title": "источник",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assertServerErrorWithMessage(t, rec)
}

// Ответ на создание вклейки — ровно те поля, что видит читатель, без
// внутреннего счёта полос: {id, document_id, work_id, status, source_title,
// created_at}. Ревьюер поймал утечку StartPageID/EndPageID прогоном, а не
// рассуждением — этот тест дословно повторяет ту проверку на самом ответе
// HTTP, а не только на типе (models.TestDocumentCutJSONHidesPageInternals
// держит контракт типа отдельно).
//
// Список наружу — БЕЛЫЙ (I2, найдено мутацией): чёрный список по именам
// Go-полей ("StartPageID" и т.п.) ловит только полное снятие json:"-" и
// переживает мутацию вида `json:"start_page_id"` вместо `json:"-"` — поле
// перестаёт называться "StartPageID" и чёрный список его не видит, а утечка
// остаётся ровно той же. Белый список требует точного множества ключей и
// вдобавок ищет само значение pages.id (999888) в теле ответа буквально —
// под любым именем поля.
func TestCreateCutResponseHidesPageInternals(t *testing.T) {
	pages := newFakePageStoreWithPages(&models.Page{
		ID: 999888, WorkID: 1, PageNumber: 4, ContentMarkdown: "page text",
	})
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		pages:     pages,
		cuts:      newFakeDocumentCutStore(),
		renderer:  markdown.NewRenderer(),
	}

	body, _ := json.Marshal(map[string]any{
		"work_id": 1, "start_page": 4, "start_offset": 0,
		"end_page": 4, "end_offset": 4, "source_title": "источник",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/documents/razbor-5/cuts", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}

	wantKeys := map[string]bool{
		"id": true, "document_id": true, "work_id": true,
		"status": true, "source_title": true, "created_at": true,
	}
	for key := range raw {
		if !wantKeys[key] {
			t.Fatalf("неожиданный ключ %q в ответе (внутреннее поле вклейки утекло наружу): %s", key, rec.Body.String())
		}
	}
	for key := range wantKeys {
		if _, ok := raw[key]; !ok {
			t.Fatalf("ожидаемый ключ %q пропал из ответа: %s", key, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), "999888") {
		t.Fatalf("pages.id (999888) утёк в ответ под каким-то именем: %s", rec.Body.String())
	}
}

// Снятая вклейка реально уходит из хранилища: Delete зовёт DeleteUnreferenced
// с keep-списком без цели — тем же приёмом, что и сборка мусора при
// сохранении разбора, только keep строится из ByDocument здесь, а не из тела.
func TestDeleteCutRemovesOnlyTargetCut(t *testing.T) {
	cuts := newFakeDocumentCutStore(
		&models.DocumentCut{ID: 1, DocumentID: 5},
		&models.DocumentCut{ID: 2, DocumentID: 5},
	)
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		cuts:      cuts,
		renderer:  markdown.NewRenderer(),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/documents/razbor-5/cuts/1", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5", "cutId": "1"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	kept := cuts.keptAfterPrune()
	if len(kept) != 1 || kept[0] != 2 {
		t.Fatalf("остались вклейки %v, ожидалась только 2", kept)
	}
}

// Снятие чужой (или несуществующей) вклейки — честный отказ, а не тихий
// успех и не 500: ByIDs сверяет document_id так же, как настоящий
// репозиторий.
func TestDeleteCutForeignCutIsNotFound(t *testing.T) {
	cuts := newFakeDocumentCutStore(&models.DocumentCut{ID: 9, DocumentID: 99})
	h := &DocumentCutHandler{
		documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5"}},
		cuts:      cuts,
		renderer:  markdown.NewRenderer(),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/documents/razbor-5/cuts/9", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "razbor-5", "cutId": "9"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey, editorClaims()))
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404: %s", rec.Code, rec.Body.String())
	}
	// Вклейка чужого разбора обязана остаться нетронутой.
	kept, _ := cuts.ByDocument(context.Background(), 99)
	if len(kept) != 1 {
		t.Fatalf("чужая вклейка задета: %v", kept)
	}
}

// Догрузка вклейки идёт тем же правилом видимости, что и сам разбор: иначе
// снятый разбор остаётся читаемым по кускам (так уже случалось у подборок —
// ItemPages и download отдавали 200 после 410 на Get).
func TestCutFullRespectsDocumentVisibility(t *testing.T) {
	doc := &models.Document{ID: 1, AuthorNickname: "чтец", Slug: "razbor", OwnerID: ptrInt64(5),
		WasPublished: true /* был опубликован и снят */}
	h := newTestCutHandler(t, doc)

	rr := doGetVars(t, h.Full, "/api/documents/чтец/razbor/cuts/1",
		map[string]string{"nickname": "чтец", "slug": "razbor", "cutId": "1"}, nil /* аноним */)
	if rr.Code != http.StatusGone {
		t.Fatalf("вклейка снятого разбора отдана анониму: код %d", rr.Code)
	}
	rr = doGetVars(t, h.Full, "/api/documents/чтец/razbor/cuts/1",
		map[string]string{"nickname": "чтец", "slug": "razbor", "cutId": "1"}, readerClaims(5, "чтец"))
	if rr.Code != http.StatusOK {
		t.Fatalf("автор не получил свою вклейку: код %d (%s)", rr.Code, rr.Body.String())
	}
}

// I3. Догрузка обязана различать РЕДАКЦИИ, а не только разборы.
//
// Full проверяет видимость разбора, но дальше отдавала вклейку по её номеру,
// сверяя только принадлежность разбору. Вклейка, на которую ссылается ТОЛЬКО
// неодобренный черновик, доставалась постороннему по перебираемому номеру: он
// узнавал, что цитирует ждущая решения правка, а подпись источника (свободный
// текст автора до 512 знаков) становилась публично читаемой мимо модерации
// вовсе — автор опубликованного разбора мог завести новых вклеек с любой
// подписью.
//
// Правило: отдаём только вклейку, НАЗВАННУЮ той редакцией, которую видит
// пришедший. Не названа — «снята», тем же кодом, что и прочие отказы этого
// маршрута (существование вклейки в чужом черновике наружу не выдаётся).
func TestCutFullServesOnlyCutsNamedByTheVisibleEdition(t *testing.T) {
	// Разбор на людях: опубликованная редакция вклейки не называет, черновик
	// (ждущий модерации) — называет.
	newDoc := func() *models.Document {
		return &models.Document{
			ID: 1, AuthorNickname: "чтец", Slug: "razbor", OwnerID: ptrInt64(5),
			PublishedAt: timePtr(time.Now()), WasPublished: true,
			PublishedTitle: "Разбор", PublishedMarkdown: "Опубликованный текст без вклейки.",
			Title:           "Разбор",
			MarkdownContent: "Правка, ждущая решения.\n\n<cut id=\"1\">\n",
			ReviewStatus:    models.DocumentPending,
		}
	}
	vars := map[string]string{"nickname": "чтец", "slug": "razbor", "cutId": "1"}

	// Посторонний видит опубликованную редакцию — вклейки в ней нет.
	rr := doGetVars(t, newTestCutHandler(t, newDoc()).Full,
		"/api/documents/чтец/razbor/cuts/1", vars, nil /* аноним */)
	if rr.Code != http.StatusGone {
		t.Fatalf("аноним получил вклейку, названную только черновиком: код %d (%s)",
			rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "источник") {
		t.Fatalf("подпись источника утекла в отказ: %s", rr.Body.String())
	}
	rr = doGetVars(t, newTestCutHandler(t, newDoc()).Full,
		"/api/documents/чтец/razbor/cuts/1", vars, readerClaims(6, "посторонний"))
	if rr.Code != http.StatusGone {
		t.Fatalf("посторонний вошедший получил вклейку черновика: код %d", rr.Code)
	}

	// Автор и модератор поданного видят черновик — и вклейку получают.
	rr = doGetVars(t, newTestCutHandler(t, newDoc()).Full,
		"/api/documents/чтец/razbor/cuts/1", vars, readerClaims(5, "чтец"))
	if rr.Code != http.StatusOK {
		t.Fatalf("автор не получил свою вклейку: код %d (%s)", rr.Code, rr.Body.String())
	}
	rr = doGetVars(t, newTestCutHandler(t, newDoc()).Full,
		"/api/documents/чтец/razbor/cuts/1", vars, editorClaims())
	if rr.Code != http.StatusOK {
		t.Fatalf("модератор поданного не получил вклейку: код %d (%s)", rr.Code, rr.Body.String())
	}

	// Ровно та же вклейка, но названная уже и опубликованной редакцией, —
	// постороннему отдаётся. Контрольная группа: отказ выше держится на
	// редакции, а не на чём-то постороннем вроде роли.
	doc := newDoc()
	doc.PublishedMarkdown = "Опубликованный текст.\n\n<cut id=\"1\">\n"
	rr = doGetVars(t, newTestCutHandler(t, doc).Full, "/api/documents/чтец/razbor/cuts/1", vars, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("вклейка опубликованной редакции не отдана анониму: код %d (%s)",
			rr.Code, rr.Body.String())
	}
}

// Завести вклейку в чужом разборе нельзя: автор решает, что цитируется.
func TestCutCreateRejectsForeignDocument(t *testing.T) {
	doc := &models.Document{ID: 1, AuthorNickname: "чтец", Slug: "razbor", OwnerID: ptrInt64(5)}
	h := newTestCutHandler(t, doc)
	rr := doPostVars(t, h.Create, "/api/documents/чтец/razbor/cuts",
		`{"work_id":1,"start_page":1,"start_offset":0,"end_page":1,"end_offset":4,"source_title":"подпись"}`,
		map[string]string{"nickname": "чтец", "slug": "razbor"}, readerClaims(6, "другой"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("вклейка заведена в чужом разборе: код %d", rr.Code)
	}
}

// Читатель, не являющийся автором разбора, тоже не может завести в нём
// вклейку — то же правило mayEditDocument, что и у Update разбора: разница
// между «чужой редактор» и «чужой читатель» роли не играет.
func TestCutCreateRejectsForEditorOnReaderDocument(t *testing.T) {
	doc := &models.Document{ID: 1, AuthorNickname: "чтец", Slug: "razbor", OwnerID: ptrInt64(5)}
	h := newTestCutHandler(t, doc)
	rr := doPostVars(t, h.Create, "/api/documents/чтец/razbor/cuts",
		`{"work_id":1,"start_page":1,"start_offset":0,"end_page":1,"end_offset":4,"source_title":"подпись"}`,
		map[string]string{"nickname": "чтец", "slug": "razbor"}, editorClaims())
	if rr.Code != http.StatusForbidden {
		t.Fatalf("редактор завёл вклейку в чужом читательском разборе: код %d (%s)", rr.Code, rr.Body.String())
	}
}

// Снятие вклейки из чужого разбора тоже запрещено — та же дыра, что и у
// Create, была бы у Delete: посторонний читатель мог бы снести вклейку из
// разбора, автором которого не является.
func TestCutDeleteRejectsForeignDocument(t *testing.T) {
	doc := &models.Document{ID: 1, AuthorNickname: "чтец", Slug: "razbor", OwnerID: ptrInt64(5)}
	h := newTestCutHandler(t, doc)
	rr := doDeleteVars(t, h.Delete, "/api/documents/чтец/razbor/cuts/1",
		map[string]string{"nickname": "чтец", "slug": "razbor", "cutId": "1"}, readerClaims(6, "другой"))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("вклейка снята из чужого разбора: код %d", rr.Code)
	}
}
