package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/fileprocessor"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

func TestParsePageRange(t *testing.T) {
	tests := []struct {
		name      string
		rangeStr  string
		maxPage   int
		want      []int
		wantError bool
	}{
		{
			name:     "single page",
			rangeStr: "5",
			maxPage:  10,
			want:     []int{5},
		},
		{
			name:     "multiple single pages",
			rangeStr: "1,3,5,7",
			maxPage:  10,
			want:     []int{1, 3, 5, 7},
		},
		{
			name:     "simple range",
			rangeStr: "1-5",
			maxPage:  10,
			want:     []int{1, 2, 3, 4, 5},
		},
		{
			name:     "mixed range and singles",
			rangeStr: "1-3,5,7-9",
			maxPage:  10,
			want:     []int{1, 2, 3, 5, 7, 8, 9},
		},
		{
			name:     "with whitespace",
			rangeStr: " 1 - 3 , 5 , 7 - 9 ",
			maxPage:  10,
			want:     []int{1, 2, 3, 5, 7, 8, 9},
		},
		{
			name:     "duplicates removed",
			rangeStr: "1,1,2,2,3,3",
			maxPage:  10,
			want:     []int{1, 2, 3},
		},
		{
			name:     "overlapping ranges",
			rangeStr: "1-5,3-7",
			maxPage:  10,
			want:     []int{1, 2, 3, 4, 5, 6, 7},
		},
		{
			name:      "page out of range high",
			rangeStr:  "15",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "page zero",
			rangeStr:  "0",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "range end exceeds max",
			rangeStr:  "8-15",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "range start below 1",
			rangeStr:  "0-5",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "inverted range",
			rangeStr:  "5-3",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "invalid page number",
			rangeStr:  "abc",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "invalid range format",
			rangeStr:  "1-2-3",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "invalid range start",
			rangeStr:  "a-5",
			maxPage:   10,
			wantError: true,
		},
		{
			name:      "invalid range end",
			rangeStr:  "1-b",
			maxPage:   10,
			wantError: true,
		},
		{
			name:     "empty parts ignored",
			rangeStr: "1,,3",
			maxPage:  10,
			want:     []int{1, 3},
		},
		{
			name:     "full range",
			rangeStr: "1-10",
			maxPage:  10,
			want:     []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		},
		{
			name:     "single page at boundary",
			rangeStr: "10",
			maxPage:  10,
			want:     []int{10},
		},
		{
			name:     "first page only",
			rangeStr: "1",
			maxPage:  10,
			want:     []int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePageRange(tt.rangeStr, tt.maxPage)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("Length = %d, want %d", len(got), len(tt.want))
				return
			}

			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("Element[%d] = %d, want %d", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestParsePageRangeSorting(t *testing.T) {
	got, err := parsePageRange("9,3,7,1,5", 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := []int{1, 3, 5, 7, 9}
	if len(got) != len(expected) {
		t.Fatalf("Length = %d, want %d", len(got), len(expected))
	}

	for i, v := range got {
		if v != expected[i] {
			t.Errorf("Element[%d] = %d, want %d (results should be sorted)", i, v, expected[i])
		}
	}
}

func createTestWorkHandler() *WorkHandler {
	return &WorkHandler{
		workRepo:      nil,
		pageRepo:      nil,
		renderer:      markdown.NewRenderer(),
		store:         storage.NewMemoryStorage(),
		presignTTL:    time.Minute,
		fileProcessor: fileprocessor.NewProcessor(),
	}
}

func TestNewWorkHandler(t *testing.T) {
	renderer := markdown.NewRenderer()
	store := storage.NewMemoryStorage()
	handler := NewWorkHandler(nil, nil, renderer, store, time.Minute, nil, nil)

	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
	if handler.renderer != renderer {
		t.Error("Expected renderer to be set")
	}
	if handler.store != store {
		t.Error("Expected store to be set")
	}
	if handler.presignTTL != time.Minute {
		t.Errorf("presignTTL = %v, want %v", handler.presignTTL, time.Minute)
	}
	if handler.fileProcessor == nil {
		t.Error("Expected fileProcessor to be created")
	}
}

func TestWorkHandler_Get_InvalidID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/works/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_Create_InvalidJSON(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works", bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_Create_NoUserContext(t *testing.T) {
	handler := createTestWorkHandler()

	body := CreateWorkRequest{
		Title:    "Test Work",
		Author:   "Test Author",
		Language: "ru",
		Country:  "Russia",
		Status:   models.WorkStatusDraft,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/works", bytes.NewBuffer(jsonBody))
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestWorkHandler_Update_InvalidID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/works/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_Update_InvalidJSON(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodPut, "/api/works/1", bytes.NewBufferString("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// Настоящий репозиторий обязан удовлетворять WorkRepo — иначе проводка в
// cmd/server/main.go развалится, а подставной репозиторий ниже проверял бы
// интерфейс, которым никто не пользуется.
var _ WorkRepo = (*repository.WorkRepository)(nil)

// fakeWorkRepo — подставной WorkRepo: помнит детей и удалённые id.
type fakeWorkRepo struct {
	children    map[int64][]*models.Work
	childrenErr error
	deleted     []int64
	created     *models.Work
	createErr   error
	role        string
}

func (f *fakeWorkRepo) List(context.Context, int, int, *models.WorkStatus, *int64) ([]*models.Work, error) {
	return nil, nil
}

func (f *fakeWorkRepo) GetByID(_ context.Context, id int64) (*models.Work, error) {
	return &models.Work{ID: id, Role: f.role}, nil
}
func (f *fakeWorkRepo) Create(_ context.Context, w *models.Work) error {
	f.created = w
	return f.createErr
}
func (f *fakeWorkRepo) Update(context.Context, *models.Work) error { return nil }

func (f *fakeWorkRepo) Delete(_ context.Context, id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeWorkRepo) ListChildren(_ context.Context, parentID int64) ([]*models.Work, error) {
	if f.childrenErr != nil {
		return nil, f.childrenErr
	}
	return f.children[parentID], nil
}
func (f *fakeWorkRepo) AddCategory(context.Context, int64, int64) error    { return nil }
func (f *fakeWorkRepo) RemoveCategory(context.Context, int64, int64) error { return nil }

func putObject(t *testing.T, store storage.Storage, key string) {
	t.Helper()
	if err := store.Put(context.Background(), key, strings.NewReader("x"), 1, "image/png"); err != nil {
		t.Fatalf("Put(%s): %v", key, err)
	}
}

func hasObject(t *testing.T, store storage.Storage, key string) bool {
	t.Helper()
	rc, err := store.Get(context.Background(), key)
	if err != nil {
		return false
	}
	rc.Close()
	return true
}

// Каскад по parent_work_id снимает строку служебной работы, но про S3 он не
// знает: без обхода детей их превью и производный PDF остались бы в бакете
// сиротами, на которые уже ничто не ссылается.
func TestWorkHandler_Delete_SnosytPrefiksySluzhebnyhRabot(t *testing.T) {
	store := storage.NewMemoryStorage()
	putObject(t, store, "works/11/original/scan.pdf")
	putObject(t, store, "works/11/pages/page_1.png")
	putObject(t, store, "works/12/original/front.pdf")
	putObject(t, store, "works/12/pages/page_1.png")
	putObject(t, store, "works/13/pages/page_1.png") // чужая работа

	repo := &fakeWorkRepo{children: map[int64][]*models.Work{
		11: {{ID: 12, ParentWorkID: ptrInt64(11), Role: "front_matter"}},
	}}
	handler := &WorkHandler{workRepo: repo, renderer: markdown.NewRenderer(),
		store: store, presignTTL: time.Minute,
		fileProcessor: fileprocessor.NewProcessor()}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/11", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "11"})
	rec := httptest.NewRecorder()
	handler.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	for _, key := range []string{"works/11/original/scan.pdf", "works/11/pages/page_1.png",
		"works/12/original/front.pdf", "works/12/pages/page_1.png"} {
		if hasObject(t, store, key) {
			t.Errorf("объект %s остался в хранилище сиротой", key)
		}
	}
	if !hasObject(t, store, "works/13/pages/page_1.png") {
		t.Error("снесён префикс посторонней работы")
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != 11 {
		t.Errorf("удалены строки %v, ожидалось [11]", repo.deleted)
	}
}

// Не сумев перечислить детей, удалять том нельзя: список после удаления
// строки взять уже неоткуда, и префиксы детей осиротеют навсегда.
func TestWorkHandler_Delete_OshibkaSpiskaDetey(t *testing.T) {
	store := storage.NewMemoryStorage()
	putObject(t, store, "works/11/pages/page_1.png")
	repo := &fakeWorkRepo{childrenErr: errors.New("boom")}
	handler := &WorkHandler{workRepo: repo, renderer: markdown.NewRenderer(),
		store: store, presignTTL: time.Minute,
		fileProcessor: fileprocessor.NewProcessor()}

	req := httptest.NewRequest(http.MethodDelete, "/api/works/11", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "11"})
	rec := httptest.NewRecorder()
	handler.Delete(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !hasObject(t, store, "works/11/pages/page_1.png") {
		t.Error("хранилище тронуто, хотя удаление отклонено")
	}
	if len(repo.deleted) != 0 {
		t.Errorf("строка удалена вопреки отказу: %v", repo.deleted)
	}
}

// Ошибка выборки детей не роняет карточку тома, но и не остаётся немой.
func TestWorkHandler_Get_OshibkaDetey_NeRonyaetKartochku(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	repo := &fakeWorkRepo{childrenErr: errors.New("boom")}
	handler := &WorkHandler{workRepo: repo, renderer: markdown.NewRenderer(),
		store: storage.NewMemoryStorage(), presignTTL: time.Minute,
		fileProcessor: fileprocessor.NewProcessor()}

	req := httptest.NewRequest(http.MethodGet, "/api/works/11", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "11"})
	rec := httptest.NewRecorder()
	handler.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(logged.String(), "boom") {
		t.Errorf("ошибка ListChildren не попала в лог: %q", logged.String())
	}
}

func TestWorkHandler_Delete_InvalidID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodDelete, "/api/works/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Delete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_AddCategory_InvalidWorkID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/invalid/categories", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.AddCategory(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_AddCategory_InvalidJSON(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/1/categories", bytes.NewBufferString("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()

	handler.AddCategory(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_RemoveCategory_InvalidWorkID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodDelete, "/api/works/invalid/categories/1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid", "categoryId": "1"})
	rec := httptest.NewRecorder()

	handler.RemoveCategory(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_RemoveCategory_InvalidCategoryID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodDelete, "/api/works/1/categories/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1", "categoryId": "invalid"})
	rec := httptest.NewRecorder()

	handler.RemoveCategory(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkHandler_UploadFile_InvalidWorkID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/invalid/upload", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.UploadFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// Сервер net/http сам сносит временные файлы формы только у исходного запроса,
// а mux отдаёт обработчику копию (r.WithContext) — форма, разобранная на копии,
// оставалась в /tmp контейнера навсегда: 4.1 ГБ оригиналов PDF на боевом за
// один день публикации. Поэтому запрос идёт через настоящий сервер и роутер, а
// файл больше порога ParseMultipartForm — иначе форма не уходит на диск вовсе.
func TestWorkHandler_UploadFile_RemovesMultipartTempFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	handler := createTestWorkHandler()
	router := mux.NewRouter()
	router.HandleFunc("/api/works/{id}/upload", handler.UploadFile)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body, contentType := largeMultipartUpload(t, "scan.txt", 40<<20)
	resp, err := http.Post(srv.URL+"/api/works/1/upload", contentType, body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Status = %d, want %d (неверное расширение)", resp.StatusCode, http.StatusBadRequest)
	}

	left, err := filepath.Glob(filepath.Join(tmp, "multipart-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("после ответа в TMPDIR остались временные файлы формы: %v", left)
	}
}

// largeMultipartUpload стримит форму с одним файлом заданного размера, не
// держа её в памяти целиком.
func largeMultipartUpload(t *testing.T, filename string, size int64) (io.Reader, string) {
	t.Helper()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filename)
		if err == nil {
			_, err = io.CopyN(part, zeroReader{}, size)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	return pr, mw.FormDataContentType()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestWorkHandler_CreatePages_InvalidWorkID(t *testing.T) {
	handler := createTestWorkHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/works/invalid/create-pages", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.CreatePages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateWorkRequest(t *testing.T) {
	req := CreateWorkRequest{
		Title:    "Test Work",
		Author:   "Test Author",
		Language: "ru",
		Country:  "Russia",
		Status:   models.WorkStatusDraft,
	}

	if req.Title != "Test Work" {
		t.Errorf("Title = %s, want Test Work", req.Title)
	}
	if req.Author != "Test Author" {
		t.Errorf("Author = %s, want Test Author", req.Author)
	}
	if req.Language != "ru" {
		t.Errorf("Language = %s, want ru", req.Language)
	}
	if req.Status != models.WorkStatusDraft {
		t.Errorf("Status = %s, want draft", req.Status)
	}
}

func TestUpdateWorkRequest(t *testing.T) {
	req := UpdateWorkRequest{
		Title:    "Updated Work",
		Author:   "Updated Author",
		Language: "en",
		Country:  "USA",
		Status:   models.WorkStatusCompleted,
	}

	if req.Title != "Updated Work" {
		t.Errorf("Title = %s, want Updated Work", req.Title)
	}
	if req.Status != models.WorkStatusCompleted {
		t.Errorf("Status = %s, want completed", req.Status)
	}
}

func TestWorkHandler_ListNotes_InvalidWorkID(t *testing.T) {
	h := &WorkHandler{renderer: markdown.NewRenderer()}
	req := httptest.NewRequest(http.MethodGet, "/api/works/invalid/notes", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	h.ListNotes(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateWorkRequest_JSON(t *testing.T) {
	jsonStr := `{"title":"Book","author":"Writer","language":"fr","country":"France","status":"in_progress"}`

	var req CreateWorkRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.Title != "Book" {
		t.Errorf("Title = %s, want Book", req.Title)
	}
	if req.Author != "Writer" {
		t.Errorf("Author = %s, want Writer", req.Author)
	}
	if req.Status != models.WorkStatusInProgress {
		t.Errorf("Status = %s, want in_progress", req.Status)
	}
}

func createWorkAsEditor(t *testing.T, repo *fakeWorkRepo, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := createTestWorkHandler()
	h.workRepo = repo
	req := httptest.NewRequest(http.MethodPost, "/api/works", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserContextKey,
		&auth.Claims{UserID: 1, Role: models.RoleEditor}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	return rec
}

// Публикатор заводит том на боевом под локальным id (спека
// 2026-10-03-local-scans-working-set, часть 3).
func TestWorkHandler_Create_PassesExplicitID(t *testing.T) {
	repo := &fakeWorkRepo{}
	rec := createWorkAsEditor(t, repo, `{"id":311,"title":"Том 22","status":"draft"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ждали 201: %s", rec.Code, rec.Body.String())
	}
	if repo.created == nil || repo.created.ID != 311 {
		t.Fatalf("в репозиторий ушло %+v, ждали id 311", repo.created)
	}
}

func TestWorkHandler_Create_WithoutIDLeavesItToSequence(t *testing.T) {
	repo := &fakeWorkRepo{}
	createWorkAsEditor(t, repo, `{"title":"Том","status":"draft"}`)
	if repo.created == nil || repo.created.ID != 0 {
		t.Fatalf("в репозиторий ушло %+v, ждали id 0", repo.created)
	}
}

func TestWorkHandler_Create_TakenIDIsConflict(t *testing.T) {
	repo := &fakeWorkRepo{createErr: fmt.Errorf("failed to create work: %w", repository.ErrIDTaken)}
	rec := createWorkAsEditor(t, repo, `{"id":311,"title":"Том 22","status":"draft"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d, ждали 409", rec.Code)
	}
}

func TestWorkHandler_Create_NegativeIDIsBadRequest(t *testing.T) {
	repo := &fakeWorkRepo{}
	rec := createWorkAsEditor(t, repo, `{"id":-5,"title":"Том","status":"draft"}`)
	if rec.Code != http.StatusBadRequest || repo.created != nil {
		t.Fatalf("код %d, вставка %+v; ждали 400 без вставки", rec.Code, repo.created)
	}
}

type fakeIssueLookup struct {
	v     *models.WorkJournalIssue
	calls int
}

func (f *fakeIssueLookup) IssueForWork(context.Context, int64) (*models.WorkJournalIssue, error) {
	f.calls++
	return f.v, nil
}

func getWork(t *testing.T, role string, lookup *fakeIssueLookup) string {
	t.Helper()
	handler := (&WorkHandler{workRepo: &fakeWorkRepo{role: role}, renderer: markdown.NewRenderer(),
		store: storage.NewMemoryStorage(), presignTTL: time.Minute,
		fileProcessor: fileprocessor.NewProcessor()}).WithJournalIssues(lookup)
	req := httptest.NewRequest(http.MethodGet, "/api/works/7", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec := httptest.NewRecorder()
	handler.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d", rec.Code)
	}
	return rec.Body.String()
}

func TestWorkGetCarriesJournalIssue(t *testing.T) {
	lookup := &fakeIssueLookup{v: &models.WorkJournalIssue{IssueID: 3, JournalSlug: "pzm",
		JournalTitle: "Под знаменем марксизма", Year: 1925, Label: "5—6"}}
	body := getWork(t, models.WorkRoleJournalIssue, lookup)
	if !strings.Contains(body, `"journal_issue":{"issue_id":3`) || !strings.Contains(body, `"journal_slug":"pzm"`) {
		t.Fatalf("нет журнальных координат: %s", body)
	}
	vol := &fakeIssueLookup{v: lookup.v}
	body = getWork(t, models.WorkRoleVolume, vol)
	if vol.calls != 0 || strings.Contains(body, "journal_issue") {
		t.Fatalf("у тома: вызовов %d, тело %s", vol.calls, body)
	}
}
