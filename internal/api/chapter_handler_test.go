package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/pkg/markdown"
)

func createTestChapterHandler() *ChapterHandler {
	return &ChapterHandler{
		chapterRepo: nil,
		pageRepo:    nil,
		renderer:    markdown.NewRenderer(),
	}
}

func TestNewChapterHandler(t *testing.T) {
	renderer := markdown.NewRenderer()
	handler := NewChapterHandler(nil, nil, renderer, NewRangeCache(pagecache.New("")))

	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
	if handler.renderer != renderer {
		t.Error("Expected renderer to be set")
	}
}

func TestChapterHandler_List_InvalidWorkID(t *testing.T) {
	handler := createTestChapterHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/invalid/chapters", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "invalid"})
	rec := httptest.NewRecorder()

	handler.List(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChapterHandler_Create_InvalidWorkID(t *testing.T) {
	handler := createTestChapterHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/invalid/chapters", nil)
	req = mux.SetURLVars(req, map[string]string{"workId": "invalid"})
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChapterHandler_Create_InvalidJSON(t *testing.T) {
	handler := createTestChapterHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/1/chapters", bytes.NewBufferString("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"workId": "1"})
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChapterHandler_Get_InvalidID(t *testing.T) {
	handler := createTestChapterHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/chapters/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChapterHandler_Update_InvalidID(t *testing.T) {
	handler := createTestChapterHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/chapters/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChapterHandler_Update_InvalidJSON(t *testing.T) {
	handler := createTestChapterHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/chapters/1", bytes.NewBufferString("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChapterHandler_Delete_InvalidID(t *testing.T) {
	handler := createTestChapterHandler()

	req := httptest.NewRequest(http.MethodDelete, "/api/chapters/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Delete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateChapterRequest(t *testing.T) {
	req := CreateChapterRequest{
		Title:       "Chapter 1",
		Type:        "chapter",
		OrderNumber: 1,
		StartPage:   1,
		EndPage:     25,
	}

	if req.Title != "Chapter 1" {
		t.Errorf("Title = %s, want Chapter 1", req.Title)
	}
	if req.Type != "chapter" {
		t.Errorf("Type = %s, want chapter", req.Type)
	}
	if req.OrderNumber != 1 {
		t.Errorf("OrderNumber = %d, want 1", req.OrderNumber)
	}
	if req.StartPage != 1 {
		t.Errorf("StartPage = %d, want 1", req.StartPage)
	}
	if req.EndPage != 25 {
		t.Errorf("EndPage = %d, want 25", req.EndPage)
	}
}

func TestCreateChapterRequest_JSON(t *testing.T) {
	jsonStr := `{"title":"Part 1","type":"part","order_number":1,"start_page":1,"end_page":50}`

	var req CreateChapterRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.Title != "Part 1" {
		t.Errorf("Title = %s, want Part 1", req.Title)
	}
	if req.Type != "part" {
		t.Errorf("Type = %s, want part", req.Type)
	}
	if req.EndPage != 50 {
		t.Errorf("EndPage = %d, want 50", req.EndPage)
	}
}

func TestChapterListPagesWiring(t *testing.T) {
	h := &ChapterHandler{
		chapterRepo: &fakeChapterStore{
			getByIDFn: func(_ context.Context, id int64) (*models.Chapter, error) {
				return &models.Chapter{ID: id, WorkID: 1, StartPage: 3, EndPage: 10}, nil
			},
		},
		pageRepo: &fakePageStore{
			getPageRangeFn: func(_ context.Context, _ int64, _, _ int) ([]*models.Page, error) {
				return []*models.Page{
					{ID: 10, PageNumber: 3, ContentMarkdown: "Alpha[^r2].\n\n[^r2]: first sub."},
					{ID: 11, PageNumber: 9, ContentMarkdown: "Beta[^r2].\n\n[^r2]: second sub."},
					{ID: 12, PageNumber: 10, ContentMarkdown: "   "},
				}, nil
			},
		},
		renderer: markdown.NewRenderer(),
	}

	req := mux.SetURLVars(httptest.NewRequest("GET", "/works/1/chapters/1/pages", nil),
		map[string]string{"workId": "1", "id": "1"})
	rec := httptest.NewRecorder()
	h.ListPages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	// Локальная структура с json-тегами: тест сверяет форму провода, а не
	// Go-тип — переименование поля в структуре без смены тега он обязан
	// пережить, смену тега — поймать.
	var got struct {
		Pages []struct {
			PageNumber int    `json:"page_number"`
			HTML       string `json:"html"`
			Blank      bool   `json:"blank"`
		} `json:"pages"`
		FootnotesHTML string `json:"footnotes_html"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad JSON: %v; body: %s", err, rec.Body.String())
	}

	if len(got.Pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(got.Pages))
	}
	// Порядок страниц сохранён и HTML сопоставлен своей странице.
	if got.Pages[0].PageNumber != 3 || !strings.Contains(got.Pages[0].HTML, "Alpha") {
		t.Errorf("page 0 mismatched: %+v", got.Pages[0])
	}
	if got.Pages[1].PageNumber != 9 || !strings.Contains(got.Pages[1].HTML, "Beta") {
		t.Errorf("page 1 mismatched: %+v", got.Pages[1])
	}
	// Бит пустой полосы считает сервер: у фронта больше нет markdown, по
	// которому он раньше решал, рисовать ли маркер номера.
	if got.Pages[0].Blank || got.Pages[1].Blank {
		t.Errorf("non-empty pages must not be blank: %+v", got.Pages)
	}
	if !got.Pages[2].Blank {
		t.Errorf("whitespace-only page must be blank: %+v", got.Pages[2])
	}
	// Полная модель страницы в ответ не едет: markdown-источник — 45% сырого
	// веса главы — дублировал бы собственную вёрстку.
	if strings.Contains(rec.Body.String(), "content_markdown") {
		t.Errorf("content_markdown must not be serialized: %s", rec.Body.String())
	}
	// Тела примечаний вырезаны из HTML страниц.
	if strings.Contains(got.Pages[0].HTML, "first sub") {
		t.Errorf("note body leaked into page HTML: %s", got.Pages[0].HTML)
	}
	// Сквозная нумерация: (1) на первой странице, (2) на второй — не (2) и (2).
	if !strings.Contains(got.Pages[0].HTML, ">(1)</a>") {
		t.Errorf("page 3 marker should be (1): %s", got.Pages[0].HTML)
	}
	if !strings.Contains(got.Pages[1].HTML, ">(2)</a>") {
		t.Errorf("page 9 marker should be (2): %s", got.Pages[1].HTML)
	}
	// Общий блок: два примечания, без постраничных заголовков.
	if !strings.Contains(got.FootnotesHTML, "Подстрочные примечания") {
		t.Errorf("missing subscript section: %s", got.FootnotesHTML)
	}
	if strings.Contains(got.FootnotesHTML, "Страница") {
		t.Errorf("notes must not be grouped by page: %s", got.FootnotesHTML)
	}
	if !strings.Contains(got.FootnotesHTML, "first sub") || !strings.Contains(got.FootnotesHTML, "second sub") {
		t.Errorf("both note bodies must be listed: %s", got.FootnotesHTML)
	}
}

// Заголовок главы длиннее колонки — ошибка КЛИЕНТА, а не сервера.
//
// `chapters.title` — VARCHAR(500) (000001_initial_schema.up.sql). До правки
// слишком длинный заголовок доезжал до Postgres, тот отвечал «value too long
// for type character varying(500)», а обработчик переводил любую ошибку
// репозитория в 500 «Failed to create chapter» простым текстом. Клиент видел
// «ошибка сервера» и не узнавал ни причины, ни того, что чинить её ему.
// Так это и вскрылось: содержание тома 3 ПСС Ленина несёт аналитические
// сводки, разбор приклеивал их к следующей записи, и четыре заголовка из 123
// вырастали за 500 знаков (самый длинный — 839).
//
// Счёт РУНАМИ: VARCHAR(500) в Postgres — 500 символов, а кириллическая буква
// в UTF-8 занимает два байта, и len() по байтам отвергал бы вдвое более
// короткий заголовок.
func TestChapterHandler_Create_TooLongTitleIsClientError(t *testing.T) {
	handler := createTestChapterHandler()

	body, _ := json.Marshal(CreateChapterRequest{
		Title:       strings.Repeat("я", chapterTitleMaxRunes+1),
		Type:        "chapter",
		OrderNumber: 1,
		StartPage:   1,
		EndPage:     2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/works/1/chapters", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workId": "1"})
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("тело ответа не JSON (%v): %s", err, rec.Body.String())
	}
	if !strings.Contains(payload["message"], "500") {
		t.Errorf("в объяснении нет предела: %q", payload["message"])
	}
}

// Ровно предел — всё ещё годный заголовок: проверка не должна отрезать
// последний допустимый символ. Репозитория у тестового обработчика нет,
// поэтому дальше проверки запрос падает на nil — но уже НЕ на 400.
func TestChapterHandler_Create_TitleExactlyAtLimitPasses(t *testing.T) {
	handler := createTestChapterHandler()

	body, _ := json.Marshal(CreateChapterRequest{
		Title: strings.Repeat("я", chapterTitleMaxRunes), Type: "chapter",
		OrderNumber: 1, StartPage: 1, EndPage: 2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/works/1/chapters", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workId": "1"})
	rec := httptest.NewRecorder()

	defer func() { _ = recover() }()
	handler.Create(rec, req)
	if rec.Code == http.StatusBadRequest {
		t.Errorf("заголовок ровно в предел отвергнут: %s", rec.Body.String())
	}
}

// Та же колонка, тот же предел — и на правке заголовка: через `Update`
// ходит нормализация заголовков содержания (apply_chapter_titles.py).
func TestChapterHandler_Update_TooLongTitleIsClientError(t *testing.T) {
	handler := createTestChapterHandler()

	body, _ := json.Marshal(CreateChapterRequest{
		Title: strings.Repeat("я", chapterTitleMaxRunes+1), Type: "chapter",
		OrderNumber: 1, StartPage: 1, EndPage: 2,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/works/1/chapters/1", bytes.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"workId": "1", "id": "1"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
