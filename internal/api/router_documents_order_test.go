package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"proofreader/internal/models"
)

// Короткий адрес с литеральным хвостом обязан стоять РАНЬШЕ длинного: иначе
// /documents/{ник}/{слаг} забирает /documents/{слаг}/view себе (считая
// "view" слагом читательского разбора), и просмотр сотруднического разбора
// уходит искать разбор со слагом «view», которого нет. Проверяется исходом
// маршрутизации, а не порядком строк в файле: переставят строки — тест
// скажет. Образец сборки — TestCollectionsMineRouteNotSwallowedByGenericSlug
// (router_access_test.go).
func TestDocumentViewNotSwallowedByReaderAddress(t *testing.T) {
	published := time.Now()
	doc := &models.Document{
		ID: 40, Title: "О государстве", MarkdownContent: "текст",
		AuthorNickname: "", Slug: "o-gosudarstve",
		PublishedTitle: "О государстве", PublishedMarkdown: "текст",
		PublishedAt: &published, WasPublished: true,
		ReviewStatus: models.DocumentApproved,
	}
	h := newTestDocumentHandler(t, doc)

	rt := newTestRouter(t)
	rt.documentHandler = h
	r := rt.Setup()

	req := httptest.NewRequest(http.MethodGet, "/api/documents/o-gosudarstve/view", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/documents/o-gosudarstve/view = %d, want 200 "+
			"(404 означало бы, что /documents/{nickname}/{slug} перехватил путь, "+
			"приняв nickname=\"o-gosudarstve\" и slug=\"view\", и не нашёл такой разбор); "+
			"тело: %s", rec.Code, rec.Body.String())
	}
	var got documentViewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; тело: %s", err, rec.Body.String())
	}
	if got.ID != 40 {
		t.Fatalf("отдан разбор id=%d, ожидался id=40 (o-gosudarstve)", got.ID)
	}
}

// Зеркало: длинный читательский адрес не должен перехватываться коротким
// сотрудническим шаблоном "/documents/{slug}" (тот требует ровно один
// сегмент и структурно не может забрать два — но регрессия в другую сторону,
// когда /{nickname}/{slug} вовсе не регистрируется или регистрируется не
// туда, тоже обязана быть видна тестом).
func TestReaderDocumentAddressReachesHandler(t *testing.T) {
	published := time.Now()
	doc := &models.Document{
		ID: 41, Title: "Что делать?", MarkdownContent: "текст",
		AuthorNickname: "chitatel", Slug: "chto-delat",
		PublishedTitle: "Что делать?", PublishedMarkdown: "текст",
		PublishedAt: &published, WasPublished: true,
		ReviewStatus: models.DocumentApproved,
	}
	h := newTestDocumentHandler(t, doc)

	rt := newTestRouter(t)
	rt.documentHandler = h
	r := rt.Setup()

	req := httptest.NewRequest(http.MethodGet, "/api/documents/chitatel/chto-delat", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/documents/chitatel/chto-delat = %d, want 200; тело: %s",
			rec.Code, rec.Body.String())
	}
	var got models.Document
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; тело: %s", err, rec.Body.String())
	}
	if got.ID != 41 {
		t.Fatalf("отдан разбор id=%d, ожидался id=41 (chitatel/chto-delat)", got.ID)
	}
}

// documentViewResponse читает из ответа View только то, что нужно этому
// тесту (id) — само View собирает более широкую форму (текст, вклейки), но
// для проверки маршрутизации достаточно поля id.
type documentViewResponse struct {
	ID int64 `json:"id"`
}
