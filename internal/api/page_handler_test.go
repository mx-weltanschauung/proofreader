package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

func createTestPageHandler() *PageHandler {
	return &PageHandler{
		pageRepo:        nil,
		pageVersionRepo: nil,
		// Update/RestoreVersion зовут reanchorPage(ctx, h.fragments, ...) и
		// reanchorDocumentCuts(ctx, h.cuts, ...) безусловно — nil здесь
		// паникует на первом же счастливом пути, а не только в тестах,
		// которые нарочно проверяют переякоривание.
		fragments:  newFakeFragmentStore(),
		cuts:       &fakeDocumentCutStore{},
		renderer:   markdown.NewRenderer(),
		store:      storage.NewMemoryStorage(),
		presignTTL: time.Minute,
	}
}

func TestNewPageHandler(t *testing.T) {
	renderer := markdown.NewRenderer()
	handler := NewPageHandler(nil, nil, newFakeFragmentStore(), &fakeDocumentCutStore{}, renderer, storage.NewMemoryStorage(), time.Minute)

	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
	if handler.renderer != renderer {
		t.Error("Expected renderer to be set")
	}
}

func TestPageHandler_List_InvalidWorkID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/invalid/pages", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "invalid"})
	rec := httptest.NewRecorder()

	handler.List(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_Get_InvalidPageID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "invalid"})
	rec := httptest.NewRecorder()

	handler.Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_Update_InvalidPageID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/works/1/pages/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "invalid"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_Update_InvalidJSON(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/works/1/pages/1", bytes.NewBufferString("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "1"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_Update_NoUserContext(t *testing.T) {
	handler := createTestPageHandler()

	body := UpdatePageRequest{
		ContentMarkdown: "# Test",
		Status:          models.PageStatusProofread,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/works/1/pages/1", bytes.NewBuffer(jsonBody))
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "1"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPageHandler_Render_InvalidPageID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/invalid/render", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "invalid"})
	rec := httptest.NewRecorder()

	handler.Render(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_ListVersions_InvalidPageID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/invalid/versions", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "invalid"})
	rec := httptest.NewRecorder()

	handler.ListVersions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_GetVersion_InvalidVersionID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/1/versions/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "1", "versionId": "invalid"})
	rec := httptest.NewRecorder()

	handler.GetVersion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_RestoreVersion_InvalidPageID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/1/pages/invalid/versions/1/restore", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "invalid", "versionId": "1"})
	rec := httptest.NewRecorder()

	handler.RestoreVersion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_RestoreVersion_InvalidVersionID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/1/pages/1/versions/invalid/restore", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "1", "versionId": "invalid"})
	rec := httptest.NewRecorder()

	handler.RestoreVersion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_RestoreVersion_NoUserContext(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/1/pages/1/versions/1/restore", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageId": "1", "versionId": "1"})
	rec := httptest.NewRecorder()

	handler.RestoreVersion(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestPageHandler_RestoreVersion_GoesThroughApplyPageEdit — сторож единства
// пути. У возврата к версии была своя копия трёх шагов (снимок в
// page_versions, запись текста, переякоривание вырезок), и переякоривание
// жило в двух местах — ровно то дублирование, ради снятия которого
// applyPageEdit и вынесена. Тест держит все три шага вместе и статус полосы
// нетронутым: возврат текста статусом не распоряжается.
//
// Шаг 3 держит ОБЕ машины, держащиеся за текст полосы (I6, найдено обзором
// ветки): раньше хранилище вклеек передавалось пустым (&fakeDocumentCutStore{})
// и ни разу не опрашивалось — тест ловил забытое переякоривание вырезок, но
// был слеп к точно такому же забвению для вклеек разбора.
func TestPageHandler_RestoreVersion_GoesThroughApplyPageEdit(t *testing.T) {
	page := &models.Page{
		ID: 42, WorkID: 14, PageNumber: 730,
		ContentMarkdown: "текст после машинной вычитки",
		Status:          models.PageStatusMachineProofread,
	}
	frags := newFakeFragmentStore()
	// Вклейка на той же полосе: голова и хвост цитаты встречаются и в
	// прежнем, и в восстанавливаемом тексте, поэтому реальный поиск после
	// восстановления обязан её найти заново, а не просто «дёрнуть метод».
	cuts := newFakeDocumentCutStore(&models.DocumentCut{
		ID: 3, DocumentID: 9,
		Anchor: models.Anchor{
			StartPageID: 42, StartOffset: 0, EndPageID: 42,
			EndOffset: len("текст после машинной вычитки"),
			HeadQuote: "текст", TailQuote: "вычитки",
		},
		Status: models.CutStatusOK,
	})
	pages := &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			if id != page.ID {
				return nil, fmt.Errorf("page %d not found", id)
			}
			return page, nil
		},
	}
	versions := &fakePageVersions{
		version: &models.PageVersion{
			ID: 7, PageID: 42, VersionNumber: 1,
			ContentMarkdown: "текст, каким он был до вычитки",
		},
	}
	h := NewPageHandler(pages, versions, frags, cuts, markdown.NewRenderer(),
		storage.NewMemoryStorage(), time.Minute)

	req := httptest.NewRequest(http.MethodPost, "/api/works/14/pages/42/versions/7/restore", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "14", "pageId": "42", "versionId": "7"})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
		&auth.Claims{UserID: 5, Role: models.RoleEditor}))
	rec := httptest.NewRecorder()

	h.RestoreVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
	}
	// Шаг 1: снимок ПРЕЖНЕГО текста с прежней пометкой.
	if len(pages.savedVersions) != 1 {
		t.Fatalf("ожидалась одна версия, создано %d", len(pages.savedVersions))
	}
	if pages.savedVersions[0].ContentMarkdown != "текст после машинной вычитки" {
		t.Fatalf("в снимок попал текст %q", pages.savedVersions[0].ContentMarkdown)
	}
	if pages.savedVersions[0].Comment != "Before restore" {
		t.Fatalf("пометка снимка %q, ожидалась %q",
			pages.savedVersions[0].Comment, "Before restore")
	}
	if pages.savedVersions[0].UserID != 5 {
		t.Fatalf("автор снимка %d, ожидался 5", pages.savedVersions[0].UserID)
	}
	// Шаг 2: текст полосы — из выбранной версии, статус не тронут.
	if page.ContentMarkdown != "текст, каким он был до вычитки" {
		t.Fatalf("текст полосы %q", page.ContentMarkdown)
	}
	if page.Status != models.PageStatusMachineProofread {
		t.Fatalf("возврат версии сменил статус полосы на %q", page.Status)
	}
	// Шаг 3: переякоривание — то самое, что забывалось при второй копии.
	if !frags.reanchoredPages[42] {
		t.Fatal("вырезки не переякорены: смещения остались привязаны к прежнему тексту")
	}
	// Вторая машина той же полосы: RestoreVersion однажды переякоривал
	// только вырезки, оставляя вклейки разбора на смещениях старого текста.
	restored := "текст, каким он был до вычитки"
	start, end, ok := cuts.movedTo(3)
	if !ok {
		t.Fatal("вклейка разбора не переякорена: возврат версии не идёт через applyPageEdit целиком")
	}
	if got := restored[start:end]; got != restored {
		t.Fatalf("вклейка переякорена на %q, ожидалось %q", got, restored)
	}
}

// pageOfWorkTestRig собирает обработчик и роутер со всеми шестью маршрутами,
// сверяющими workId полосы: полоса 7 принадлежит работе 1. Тесты дальше
// спрашивают её то у работы 1 (своя), то у работы 142 (чужая).
func pageOfWorkTestRig(t *testing.T) (*models.Page, http.Handler) {
	t.Helper()
	page := &models.Page{
		ID: 7, WorkID: 1, PageNumber: 3, ContentMarkdown: "текст полосы",
		Status: models.PageStatusNotProofread,
	}
	pages := &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			if id != page.ID {
				return nil, fmt.Errorf("page %d not found", id)
			}
			return page, nil
		},
	}
	versions := &fakePageVersions{
		version: &models.PageVersion{ID: 99, PageID: page.ID, VersionNumber: 1, ContentMarkdown: "прежний текст"},
	}
	h := NewPageHandler(pages, versions, newFakeFragmentStore(), &fakeDocumentCutStore{}, markdown.NewRenderer(),
		storage.NewMemoryStorage(), time.Minute)

	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/pages/{pageId}", h.Get).Methods("GET")
	r.HandleFunc("/api/works/{workId}/pages/{pageId}", h.Update).Methods("PUT")
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/render", h.Render).Methods("GET")
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/versions", h.ListVersions).Methods("GET")
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/versions/{versionId}", h.GetVersion).Methods("GET")
	r.HandleFunc("/api/works/{workId}/pages/{pageId}/versions/{versionId}/restore", h.RestoreVersion).Methods("POST")
	return page, r
}

// authedRequest прикладывает контекст редактора — Update и RestoreVersion без
// него отвечают 401 раньше, чем дойдут до сверки workId.
func authedRequest(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
		&auth.Claims{UserID: 1, Role: models.RoleEditor}))
}

// Номер работы в пути — координата, а не украшение: /works/142/pages/233
// отдавал полосу работы 1. Прецедент сверки стоит рядом — canonicalChapter
// (seo/canonical.go:69) сверяет ch.WorkID != workID ровно потому, что здесь
// не сверялось. Чинятся все шесть обработчиков разом: оставить PUT
// несверенным значило бы оставить тот же дефект, но с записью.
func TestPageHandlerRejectsPageOfAnotherWork(t *testing.T) {
	updateBody := bytes.NewBufferString(`{"content_markdown":"новый текст","status":"не_вычитана"}`)

	// полоса 7 принадлежит работе 1; спрашиваем её у работы 142
	for _, tc := range []struct {
		name, method, path string
		body               *bytes.Buffer
		authed             bool
	}{
		{"get", "GET", "/api/works/142/pages/7", nil, false},
		{"render", "GET", "/api/works/142/pages/7/render", nil, false},
		{"versions", "GET", "/api/works/142/pages/7/versions", nil, false},
		{"get_version", "GET", "/api/works/142/pages/7/versions/99", nil, false},
		{"update", "PUT", "/api/works/142/pages/7", updateBody, true},
		{"restore", "POST", "/api/works/142/pages/7/versions/99/restore", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r := pageOfWorkTestRig(t)
			var body *bytes.Buffer
			if tc.body != nil {
				body = bytes.NewBufferString(tc.body.String())
			} else {
				body = bytes.NewBufferString("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.authed {
				req = authedRequest(req)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("код %d, ожидался 404: полоса чужой работы; тело %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Обратная половина: своя полоса своей работы по-прежнему отдаётся и
// правится. Без неё проверка «всегда 404» прошла бы тоже.
func TestPageHandlerServesPageOfItsOwnWork(t *testing.T) {
	updateBody := `{"content_markdown":"новый текст","status":"не_вычитана"}`

	for _, tc := range []struct {
		name, method, path string
		body               string
		authed             bool
	}{
		{"get", "GET", "/api/works/1/pages/7", "", false},
		{"render", "GET", "/api/works/1/pages/7/render", "", false},
		{"versions", "GET", "/api/works/1/pages/7/versions", "", false},
		{"get_version", "GET", "/api/works/1/pages/7/versions/99", "", false},
		{"update", "PUT", "/api/works/1/pages/7", updateBody, true},
		{"restore", "POST", "/api/works/1/pages/7/versions/99/restore", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r := pageOfWorkTestRig(t)
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			if tc.authed {
				req = authedRequest(req)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("код %d, ожидался 200; тело %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// versionOfPageTestRig — как pageOfWorkTestRig, но для другой координаты
// пути: версия. Полоса 7 (работы 1) владеет версией 99; версия 55
// принадлежит полосе 999, которой в этом фейке вообще нет — путь
// /works/1/pages/7/versions/55 адресует версию, живущую на чужой полосе.
// pages.savedVersions — счётчик побочного эффекта на запись: GetVersion при
// подмене всего лишь читает не туда, а RestoreVersion пишет чужой текст
// поверх полосы 7, и тест обязан ловить оба случая. Счётчик один, потому что
// снимок и запись текста теперь одно действие (PageStore.SaveEdit).
func versionOfPageTestRig(t *testing.T) (page *models.Page, r http.Handler, pages *fakePageStore) {
	t.Helper()
	page = &models.Page{
		ID: 7, WorkID: 1, PageNumber: 3, ContentMarkdown: "текст полосы",
		Status: models.PageStatusNotProofread,
	}
	pages = &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			if id != page.ID {
				return nil, fmt.Errorf("page %d not found", id)
			}
			return page, nil
		},
	}
	versions := &fakePageVersions{
		version: &models.PageVersion{ID: 99, PageID: page.ID, VersionNumber: 1, ContentMarkdown: "прежний текст"},
		versions: map[int64]*models.PageVersion{
			55: {ID: 55, PageID: 999, VersionNumber: 1, ContentMarkdown: "текст чужой полосы"},
		},
	}
	h := NewPageHandler(pages, versions, newFakeFragmentStore(), &fakeDocumentCutStore{}, markdown.NewRenderer(),
		storage.NewMemoryStorage(), time.Minute)

	router := mux.NewRouter()
	router.HandleFunc("/api/works/{workId}/pages/{pageId}/versions/{versionId}", h.GetVersion).Methods("GET")
	router.HandleFunc("/api/works/{workId}/pages/{pageId}/versions/{versionId}/restore", h.RestoreVersion).Methods("POST")
	return page, router, pages
}

// Находка фикс-раунда 1: GetVersion и RestoreVersion сверяют полосу с
// работой (pageOfWork), но версию с полосой — нет. versionId адресуется сам
// по себе, независимо от того, чья это версия: GET отдал бы содержимое
// версии другой полосы, а POST .../restore записал бы это содержимое поверх
// текста ЭТОЙ полосы.
func TestPageHandlerRejectsVersionOfAnotherPage(t *testing.T) {
	page, r, pages := versionOfPageTestRig(t)

	t.Run("get_version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/7/versions/55", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("код %d, ожидался 404: версия чужой полосы; тело %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("restore", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/works/1/pages/7/versions/55/restore", nil))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("код %d, ожидался 404: версия чужой полосы; тело %s", rec.Code, rec.Body.String())
		}
		if len(pages.savedVersions) != 0 {
			t.Error("снимок версии создан — restore начал писать раньше сверки версии с полосой")
		}
		if page.ContentMarkdown != "текст полосы" {
			t.Errorf("текст полосы изменился на %q — восстановлен чужой текст", page.ContentMarkdown)
		}
	})
}

// Обратная половина: версия, действительно принадлежащая своей полосе,
// по-прежнему читается и восстанавливается. Без неё проверка «всегда 404»
// прошла бы тоже.
func TestPageHandlerServesVersionOfItsOwnPage(t *testing.T) {
	_, r, pages := versionOfPageTestRig(t)

	t.Run("get_version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/7/versions/99", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d, ожидался 200; тело %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("restore", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/works/1/pages/7/versions/99/restore", nil))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d, ожидался 200; тело %s", rec.Code, rec.Body.String())
		}
		if len(pages.savedVersions) != 1 {
			t.Errorf("снимков создано %d, ожидался 1", len(pages.savedVersions))
		}
	})
}

func TestCreatePageRequest(t *testing.T) {
	req := CreatePageRequest{
		PageNumber:      42,
		ContentMarkdown: "# Page Content",
		Status:          models.PageStatusNotProofread,
	}

	if req.PageNumber != 42 {
		t.Errorf("PageNumber = %d, want 42", req.PageNumber)
	}
	if req.ContentMarkdown != "# Page Content" {
		t.Errorf("ContentMarkdown = %s, want # Page Content", req.ContentMarkdown)
	}
	if req.Status != models.PageStatusNotProofread {
		t.Errorf("Status = %s, want not_proofread", req.Status)
	}
}

func TestUpdatePageRequest(t *testing.T) {
	req := UpdatePageRequest{
		ContentMarkdown: "# Updated Content",
		Status:          models.PageStatusProofread,
		Comment:         "Fixed typos",
	}

	if req.ContentMarkdown != "# Updated Content" {
		t.Errorf("ContentMarkdown mismatch")
	}
	if req.Status != models.PageStatusProofread {
		t.Errorf("Status = %s, want proofread", req.Status)
	}
	if req.Comment != "Fixed typos" {
		t.Errorf("Comment = %s, want Fixed typos", req.Comment)
	}
}

func TestUpdatePageRequest_JSON(t *testing.T) {
	jsonStr := `{"content_markdown":"# Test","status":"вычитана","comment":"done"}`

	var req UpdatePageRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.ContentMarkdown != "# Test" {
		t.Errorf("ContentMarkdown = %s, want # Test", req.ContentMarkdown)
	}
	if req.Comment != "done" {
		t.Errorf("Comment = %s, want done", req.Comment)
	}
}

func createTestClaimsContext(userID int64, role models.UserRole) context.Context {
	cfg := &config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	}
	_ = auth.NewService(cfg)

	claims := &auth.Claims{
		UserID: userID,
		Email:  "test@example.com",
		Role:   role,
	}

	return context.WithValue(context.Background(), middleware.UserContextKey, claims)
}

func TestPageHandler_GetByNumber_InvalidWorkID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/invalid/pages/by-number/245", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "invalid", "pageNumber": "245"})
	rec := httptest.NewRecorder()

	handler.GetByNumber(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_GetByNumber_InvalidPageNumber(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/1/pages/by-number/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "pageNumber": "invalid"})
	rec := httptest.NewRecorder()

	handler.GetByNumber(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageHandler_GetByNumber_NotFound(t *testing.T) {
	h := &PageHandler{
		pageRepo: &fakePageStore{
			getByWorkAndPageNumberFn: func(_ context.Context, _ int64, _ int) (*models.Page, error) {
				return nil, errors.New("page not found")
			},
		},
		store:      storage.NewMemoryStorage(),
		presignTTL: time.Minute,
	}

	req := mux.SetURLVars(
		httptest.NewRequest(http.MethodGet, "/api/works/3/pages/by-number/9999", nil),
		map[string]string{"workId": "3", "pageNumber": "9999"})
	rec := httptest.NewRecorder()

	h.GetByNumber(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// Номер страницы уникален внутри работы, поэтому резолвер отдаёт ту же
// сущность, что и GET по ID, — включая сам ID, по которому фронт дальше
// дёргает render/update/versions.
func TestPageHandler_GetByNumber_ReturnsPage(t *testing.T) {
	var gotWorkID int64
	var gotPageNumber int

	h := &PageHandler{
		pageRepo: &fakePageStore{
			getByWorkAndPageNumberFn: func(_ context.Context, workID int64, pageNumber int) (*models.Page, error) {
				gotWorkID, gotPageNumber = workID, pageNumber
				return &models.Page{ID: 1882, WorkID: workID, PageNumber: pageNumber}, nil
			},
		},
		store:      storage.NewMemoryStorage(),
		presignTTL: time.Minute,
	}

	req := mux.SetURLVars(
		httptest.NewRequest(http.MethodGet, "/api/works/3/pages/by-number/245", nil),
		map[string]string{"workId": "3", "pageNumber": "245"})
	rec := httptest.NewRecorder()

	h.GetByNumber(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotWorkID != 3 || gotPageNumber != 245 {
		t.Errorf("repo called with (%d, %d), want (3, 245)", gotWorkID, gotPageNumber)
	}

	var got models.Page
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; body: %s", err, rec.Body.String())
	}
	if got.ID != 1882 {
		t.Errorf("id = %d, want 1882", got.ID)
	}
	if got.PageNumber != 245 {
		t.Errorf("page_number = %d, want 245", got.PageNumber)
	}
}

// fakePageVersions — подставной репозиторий версий: обработчик страницы
// пишет версию перед каждой правкой, и без него Update не проходит.
type fakePageVersions struct {
	// Записи здесь нет: снимок пишется вместе с текстом полосы, и считает
	// снимки fakePageStore.savedVersions. Свой счётчик тут лгал бы — он
	// остался бы нулём при настоящей правке.
	version *models.PageVersion
	// versions — дополнительные версии сверх version, по своему id. Нужны,
	// чтобы выразить версию, принадлежащую ЧУЖОЙ полосе: настоящий
	// PageVersionRepository.GetByID всегда ищет по id (WHERE id = $1) и
	// никогда не подставляет произвольную версию вместо запрошенной —
	// раньше фейк именно так и делал (игнорировал id, всегда отдавал
	// f.version), из-за чего сверку версии с полосой нельзя было выразить
	// ни одним тестом.
	versions map[int64]*models.PageVersion
	latest   int
}

func (f *fakePageVersions) GetByID(ctx context.Context, id int64) (*models.PageVersion, error) {
	if f.version != nil && f.version.ID == id {
		return f.version, nil
	}
	if v, ok := f.versions[id]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("page version %d not found", id)
}

func (f *fakePageVersions) ListByPage(ctx context.Context, pageID int64) ([]*models.PageVersion, error) {
	return nil, nil
}

// latest — номер последней версии полосы; следующая получит latest+1.
func (f *fakePageVersions) GetLatestVersionNumber(ctx context.Context, pageID int64) (int, error) {
	return f.latest, nil
}

// pageHandlerWithFragments собирает обработчик страниц на подставных
// хранилищах: реальная страница живёт в замыканиях fakePageStore.
func pageHandlerWithFragments(t *testing.T, page *models.Page, frags *fakeFragmentStore) *PageHandler {
	t.Helper()
	pages := &fakePageStore{
		getByIDFn: func(ctx context.Context, id int64) (*models.Page, error) {
			if id != page.ID {
				return nil, fmt.Errorf("page %d not found", id)
			}
			return page, nil
		},
	}
	return NewPageHandler(pages, &fakePageVersions{}, frags, &fakeDocumentCutStore{}, markdown.NewRenderer(),
		storage.NewMemoryStorage(), time.Minute)
}

// updatePageRequest шлёт PUT страницы от имени редактора.
func updatePageRequest(t *testing.T, h *PageHandler, pageID int64, markdownText string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"content_markdown": markdownText,
		"status":           string(models.PageStatusNotProofread),
	})
	if err != nil {
		t.Fatalf("собрать тело: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut,
		fmt.Sprintf("/api/works/14/pages/%d", pageID), bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workId": "14", "pageId": fmt.Sprint(pageID)})
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
		&auth.Claims{UserID: 1, Role: models.RoleEditor}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	return rec
}

func TestUpdateReanchorsFragments(t *testing.T) {
	// Правка выше вырезки двигает её смещения; вырезка, чью цитату
	// переписали, честно уходит в stale.
	frags := newFakeFragmentStore()
	frags.byPage[42] = []*models.IndexFragment{
		{
			ID: 91, StartPageID: 42, EndPageID: 42, StartOffset: 0, EndOffset: len("первый абзац"),
			HeadQuote: "первый абзац", TailQuote: "первый абзац",
			Status: models.FragmentStatusConfirmed,
		},
		{
			ID: 92, StartPageID: 42, EndPageID: 42,
			StartOffset: len("первый абзац\n\n"),
			EndOffset:   len("первый абзац\n\nвторой абзац"),
			HeadQuote:   "второй абзац", TailQuote: "второй абзац",
			Status: models.FragmentStatusMachine,
		},
	}
	page := &models.Page{
		ID: 42, WorkID: 14, PageNumber: 730,
		ContentMarkdown: "первый абзац\n\nвторой абзац",
		Status:          models.PageStatusNotProofread,
	}
	h := pageHandlerWithFragments(t, page, frags)

	rec := updatePageRequest(t, h, 42, "вставка\n\nпервый абзац\n\nВТОРОЙ АБЗАЦ")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
	}
	if _, ok := frags.anchors[91]; !ok {
		t.Fatal("уцелевшая вырезка не переякорена")
	}
	if frags.statuses[92] != models.FragmentStatusStale {
		t.Fatalf("вырезка с переписанной цитатой получила статус %q", frags.statuses[92])
	}
}

func TestPageHandler_PageMap(t *testing.T) {
	handler := createTestPageHandler()
	handler.pageRepo = &fakePageStore{
		listPageMapFn: func(_ context.Context, workID int64) ([]models.PageMapEntry, error) {
			if workID != 41 {
				t.Errorf("workID = %d, want 41", workID)
			}
			return []models.PageMapEntry{
				{PageNumber: 1, Status: models.PageStatusNotProofread},
				{PageNumber: 2, Status: models.PageStatusMachineProofread},
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/works/41/page-map", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "41"})
	rec := httptest.NewRecorder()

	handler.PageMap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []models.PageMapEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 2 || got[0].PageNumber != 1 || got[1].Status != models.PageStatusMachineProofread {
		t.Errorf("got = %+v", got)
	}
}

// Пустой массив, а не null: списочные маршруты проекта уже отдают null, и
// клиенту приходится проверять это на каждом вызове.
func TestPageHandler_PageMap_EmptyIsArray(t *testing.T) {
	handler := createTestPageHandler()
	handler.pageRepo = &fakePageStore{
		listPageMapFn: func(_ context.Context, _ int64) ([]models.PageMapEntry, error) {
			return nil, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/works/7/page-map", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "7"})
	rec := httptest.NewRecorder()

	handler.PageMap(rec, req)

	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

func TestPageHandler_PageMap_InvalidWorkID(t *testing.T) {
	handler := createTestPageHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/invalid/page-map", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "invalid"})
	rec := httptest.NewRecorder()

	handler.PageMap(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageMap_RepositoryError(t *testing.T) {
	handler := createTestPageHandler()
	handler.pageRepo = &fakePageStore{
		listPageMapFn: func(_ context.Context, _ int64) ([]models.PageMapEntry, error) {
			return nil, errors.New("boom")
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/works/41/page-map", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "41"})
	rec := httptest.NewRecorder()

	handler.PageMap(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestUpdateSucceedsWhenReanchorFails(t *testing.T) {
	// Переякоривание — служебная работа поверх правки. Сбой в нём не должен
	// откатывать сохранённую страницу: текст уже записан, и потерять его
	// хуже, чем оставить вырезки на старых смещениях до следующей правки.
	frags := newFakeFragmentStore()
	frags.failOnGet = true
	page := &models.Page{ID: 42, WorkID: 14, PageNumber: 730, ContentMarkdown: "было"}
	h := pageHandlerWithFragments(t, page, frags)

	rec := updatePageRequest(t, h, 42, "стало")

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
	}
	if page.ContentMarkdown != "стало" {
		t.Fatalf("страница не сохранена: %q", page.ContentMarkdown)
	}
}
