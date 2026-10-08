package api

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

// downloadRequest прогоняет запрос через обработчик с проставленными
// переменными маршрута — иначе mux.Vars пуст и обработчик отвечает 400.
func downloadRequest(h http.HandlerFunc, target string, vars map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = mux.SetURLVars(req, vars)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// downloadHandler — обработчик поверх downloadCollectionFixture()
// (download_source_test.go, Task 9—10): том 1 «Том 23» с главой 10, и подборка
// «moi» / «Моя подборка» поверх той же работы — всё, что нужно трём маршрутам
// сразу.
func downloadHandler() *DownloadHandler {
	return NewDownloadHandler(downloadCollectionFixture())
}

func TestDownloadWorkServesEPUB(t *testing.T) {
	rec := downloadRequest(downloadHandler().Work, "/api/works/1/download?format=epub", map[string]string{"id": "1"})

	if rec.Code != http.StatusOK {
		t.Fatalf("статус = %d, хотел 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/epub+zip" {
		t.Errorf("Content-Type = %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Error("пустое тело ответа")
	}
}

func TestDownloadRejectsUnknownFormat(t *testing.T) {
	rec := downloadRequest(downloadHandler().Work, "/api/works/1/download?format=docx", map[string]string{"id": "1"})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("статус = %d, хотел 400", rec.Code)
	}
	// Читателю должно быть видно, что вообще можно попросить.
	if !strings.Contains(rec.Body.String(), "epub") {
		t.Errorf("в ответе не перечислены форматы: %s", rec.Body.String())
	}
}

func TestDownloadRequiresFormat(t *testing.T) {
	rec := downloadRequest(downloadHandler().Work, "/api/works/1/download", map[string]string{"id": "1"})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("статус = %d, хотел 400", rec.Code)
	}
}

func TestDownloadMissingWorkAnswers404(t *testing.T) {
	src := downloadCollectionFixture()
	src.works.(*fakeWorkStore).getFn = nil // отдаёт nil без ошибки

	rec := downloadRequest(NewDownloadHandler(src).Work,
		"/api/works/999/download?format=md", map[string]string{"id": "999"})

	if rec.Code != http.StatusNotFound {
		t.Errorf("статус = %d, хотел 404", rec.Code)
	}
}

func TestDownloadInvalidIDAnswers400(t *testing.T) {
	rec := downloadRequest(downloadHandler().Work,
		"/api/works/abc/download?format=md", map[string]string{"id": "abc"})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("статус = %d, хотел 400", rec.Code)
	}
}

// Заголовок обязан нести оба имени и разбираться штатным разборщиком.
func TestDownloadSetsBothFileNames(t *testing.T) {
	rec := downloadRequest(downloadHandler().Work, "/api/works/1/download?format=fb2", map[string]string{"id": "1"})

	disposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, `filename="work-1.fb2"`) {
		t.Errorf("нет ASCII-запаски: %q", disposition)
	}
	_, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		t.Fatalf("заголовок не разбирается: %v (%q)", err, disposition)
	}
	if params["filename"] != "Том 23.fb2" {
		t.Errorf("человеческое имя = %q, хотел «Том 23.fb2»", params["filename"])
	}
}

func TestDownloadChapterServesMarkdown(t *testing.T) {
	rec := downloadRequest(downloadHandler().Chapter,
		"/api/works/1/chapters/10/download?format=md",
		map[string]string{"workId": "1", "id": "10"})

	if rec.Code != http.StatusOK {
		t.Fatalf("статус = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Начало главы.") {
		t.Errorf("текст главы не попал в файл:\n%s", rec.Body.String())
	}
}

func TestDownloadCollectionServesHTML(t *testing.T) {
	rec := downloadRequest(downloadHandler().Collection,
		"/api/collections/moi/download?format=html", map[string]string{"slug": "moi"})

	if rec.Code != http.StatusOK {
		t.Fatalf("статус = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Моя подборка") {
		t.Error("название подборки не попало в файл")
	}
}

// ETag держится на воспроизводимости выгрузки; повторный запрос с
// If-None-Match не должен пересобирать том.
func TestDownloadAnswers304OnMatchingETag(t *testing.T) {
	h := downloadHandler()
	first := downloadRequest(h.Work, "/api/works/1/download?format=md", map[string]string{"id": "1"})

	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag не выставлен")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/works/1/download?format=md", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.Work(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Errorf("статус = %d, хотел 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Error("на 304 отдано тело")
	}
}

func TestDownloadETagDiffersByFormat(t *testing.T) {
	h := downloadHandler()
	md := downloadRequest(h.Work, "/api/works/1/download?format=md", map[string]string{"id": "1"})
	epub := downloadRequest(h.Work, "/api/works/1/download?format=epub", map[string]string{"id": "1"})

	if md.Header().Get("ETag") == epub.Header().Get("ETag") {
		t.Error("ETag одинаков для разных форматов — читатель получит не тот файл из кэша")
	}
}

// downloadCollectionVisibilityFixture — та же подборка, что использует
// downloadCollectionFixture() для сборки книги, но с гейтящими полями
// (владелец, published/everPublished), которые проверяет collectionVisibleTo.
func downloadCollectionVisibilityFixture(published, everPublished bool) *DownloadSource {
	src := downloadCollectionFixture()
	owner := testCollectionOwnerClaims.UserID
	c := &models.Collection{
		ID: 5, Title: "Моя подборка", Slug: "moi",
		OwnerID: &owner, AuthorNickname: testCollectionOwnerClaims.Nickname,
	}
	if published {
		c.PublishedAt = ptrTime(time.Now().Add(-time.Hour))
	}
	if everPublished {
		c.PublishIPHash = "была-отметка"
	}
	src.collections.(*fakeCollectionStoreForDownload).collection = c
	return src
}

// TestDownloadCollectionHidesDraftFromStranger — БЛОКЕР рецензии: скачивание
// не проверяло видимость вовсе, поэтому черновик, никогда не публиковавшийся,
// отдавал файл 200 постороннему.
func TestDownloadCollectionHidesDraftFromStranger(t *testing.T) {
	src := downloadCollectionVisibilityFixture(false, false)
	rec := downloadRequest(NewDownloadHandler(src).Collection,
		"/api/collections/хранитель/moi/download?format=html",
		map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "moi"})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404 — черновик не должен скачиваться постороннему; тело: %s",
			rec.Code, rec.Body.String())
	}
}

// TestDownloadCollectionHidesUnpublishedFromStranger — тот же блокер, вторая
// половина: «снять с публикации» обязано закрыть и скачивание, а не только
// адрес самой подборки.
func TestDownloadCollectionHidesUnpublishedFromStranger(t *testing.T) {
	src := downloadCollectionVisibilityFixture(false, true)
	rec := downloadRequest(NewDownloadHandler(src).Collection,
		"/api/collections/хранитель/moi/download?format=html",
		map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "moi"})

	if rec.Code != http.StatusGone {
		t.Fatalf("код %d, ожидался 410 — снятая с публикации подборка не должна скачиваться; тело: %s",
			rec.Code, rec.Body.String())
	}
}

// TestDownloadCollectionVisibleToOwnerDraft — владелец обязан по-прежнему
// скачивать свой черновик, гейт не должен закрыть маршрут и ему.
func TestDownloadCollectionVisibleToOwnerDraft(t *testing.T) {
	src := downloadCollectionVisibilityFixture(false, false)
	req := httptest.NewRequest(http.MethodGet, "/api/collections/хранитель/moi/download?format=html", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": testCollectionOwnerClaims.Nickname, "slug": "moi"})
	rec := httptest.NewRecorder()
	withClaims(testCollectionOwnerClaims, NewDownloadHandler(src).Collection)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200 — владелец скачивает свой черновик; тело: %s", rec.Code, rec.Body.String())
	}
}
