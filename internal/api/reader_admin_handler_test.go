package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// fakeReaderStore — склад читателей в памяти. Запоминает аргументы вызова:
// именно через них проверяется, что обработчик донёс строку поиска до запроса,
// а не отфильтровал её сам.
type fakeReaderStore struct {
	rows      []repository.ReaderRow
	gotQuery  string
	gotLimit  int
	failWith  error
	callCount int
}

func (f *fakeReaderStore) ListReaders(
	ctx context.Context, query string, limit int,
) ([]repository.ReaderRow, error) {
	f.callCount++
	f.gotQuery = query
	f.gotLimit = limit
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.rows, nil
}

// fakeAuthorCollectionStore — подборки по снимку ника.
type fakeAuthorCollectionStore struct {
	byNickname  map[string][]*models.Collection
	gotNickname string
	failWith    error
}

func (f *fakeAuthorCollectionStore) ListByAuthorNickname(
	ctx context.Context, nickname string,
) ([]*models.Collection, error) {
	f.gotNickname = nickname
	if f.failWith != nil {
		return nil, f.failWith
	}
	return f.byNickname[nickname], nil
}

func newReaderAdminTestHandler(
	readers *fakeReaderStore, collections *fakeAuthorCollectionStore,
) *ReaderAdminHandler {
	return NewReaderAdminHandler(readers, collections)
}

// Строка поиска доезжает до запроса как есть: обработчик её не режет и не
// нормализует — нормализация живёт в SQL, рядом с ключом уникальности ника.
func TestReaderAdminListPassesQueryToStore(t *testing.T) {
	readers := &fakeReaderStore{rows: []repository.ReaderRow{
		{ID: 7, Nickname: "Чтец", CreatedAt: time.Now(), CollectionCount: 2},
	}}
	h := newReaderAdminTestHandler(readers, &fakeAuthorCollectionStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/readers?q=%D0%A7%D0%A2%D0%95%D0%A6", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждём 200: %s", rec.Code, rec.Body.String())
	}
	if readers.gotQuery != "ЧТЕЦ" {
		t.Errorf("склад получил строку поиска %q, ждём «ЧТЕЦ»", readers.gotQuery)
	}
	if readers.gotLimit <= 0 {
		t.Errorf("склад получил предел %d, ждём положительный", readers.gotLimit)
	}

	var got []repository.ReaderRow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("ответ не разбирается как список читателей: %v (%s)", err, rec.Body.String())
	}
	if len(got) != 1 || got[0].Nickname != "Чтец" || got[0].CollectionCount != 2 {
		t.Fatalf("ответ %+v, ждём одного «Чтец» с двумя подборками", got)
	}
}

// Пустой список обязан приехать пустым массивом: клиент ждёт [], а не null.
func TestReaderAdminListReturnsEmptyArray(t *testing.T) {
	h := newReaderAdminTestHandler(&fakeReaderStore{}, &fakeAuthorCollectionStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/readers", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if body := rec.Body.String(); body != "[]\n" && body != "[]" {
		t.Fatalf("пустой список приехал как %q, ждём []", body)
	}
}

// Отказ склада — 500 с русским сообщением в JSON: apiError.ts разбирает
// только {"message": …}, text/plain от http.Error до пользователя не доходит.
func TestReaderAdminListFailureIsJSONMessage(t *testing.T) {
	readers := &fakeReaderStore{failWith: errors.New("склад недоступен")}
	h := newReaderAdminTestHandler(readers, &fakeAuthorCollectionStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/readers", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, ждём 500", rec.Code)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Message == "" {
		t.Fatalf("в ответе нет поля message: %s", rec.Body.String())
	}
}

// Ник приезжает в пути кириллицей и достаётся из переменных маршрута.
// Черновик читателя администратор обязан видеть: жалуются и на неопубликованное.
func TestReaderAdminCollectionsReturnsAllOfNickname(t *testing.T) {
	published := time.Now()
	collections := &fakeAuthorCollectionStore{byNickname: map[string][]*models.Collection{
		"Чтец": {
			{ID: 1, Title: "Ранний Маркс", Slug: "ranniy-marks", AuthorNickname: "Чтец", PublishedAt: &published},
			{ID: 2, Title: "Черновик", Slug: "chernovik", AuthorNickname: "Чтец"},
		},
	}}
	h := newReaderAdminTestHandler(&fakeReaderStore{}, collections)

	req := httptest.NewRequest(http.MethodGet, "/api/readers/%D0%A7%D1%82%D0%B5%D1%86/collections", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": "Чтец"})
	rec := httptest.NewRecorder()
	h.Collections(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждём 200: %s", rec.Code, rec.Body.String())
	}
	if collections.gotNickname != "Чтец" {
		t.Errorf("склад получил ник %q, ждём «Чтец»", collections.gotNickname)
	}
	var got []*models.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("ответ не разбирается как список подборок: %v (%s)", err, rec.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("приехало %d подборок, ждём 2 (опубликованную и черновик)", len(got))
	}
	if got[1].PublishedAt != nil {
		t.Errorf("черновик приехал опубликованным: %+v", got[1])
	}
}

// Снятая с публикации и никогда не публиковавшаяся — для разбора жалобы это
// разные вещи: снятую читатели видели, черновика не видел никто, кроме автора.
// Отличает их publish_ip_hash, но он `json:"-"` (отметка адреса наружу не
// уезжает), поэтому признак считает сервер отдельным полем.
func TestReaderAdminCollectionsMarksWithdrawn(t *testing.T) {
	collections := &fakeAuthorCollectionStore{byNickname: map[string][]*models.Collection{
		"Чтец": {
			{ID: 1, Title: "Черновик", Slug: "chernovik", AuthorNickname: "Чтец"},
			{ID: 2, Title: "Снятая", Slug: "snyataya", AuthorNickname: "Чтец", PublishIPHash: "отметка"},
		},
	}}
	h := newReaderAdminTestHandler(&fakeReaderStore{}, collections)

	req := httptest.NewRequest(http.MethodGet, "/api/readers/x/collections", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": "Чтец"})
	rec := httptest.NewRecorder()
	h.Collections(rec, req)

	var got []struct {
		Slug         string `json:"slug"`
		Title        string `json:"title"`
		WasPublished bool   `json:"was_published"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("ответ не разбирается: %v (%s)", err, rec.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("приехало %d подборок, ждём 2", len(got))
	}
	if got[0].WasPublished {
		t.Errorf("черновик помечен публиковавшимся: %+v", got[0])
	}
	if !got[1].WasPublished {
		t.Errorf("снятая с публикации не помечена публиковавшейся: %+v", got[1])
	}
	if strings.Contains(rec.Body.String(), "отметка") {
		t.Errorf("отметка адреса публикации уехала наружу: %s", rec.Body.String())
	}
}

// Ник без подборок — пустой массив, а не null и не 404: список подборок ника
// отвечает о подборках, а не о существовании учётной записи.
func TestReaderAdminCollectionsReturnsEmptyArray(t *testing.T) {
	h := newReaderAdminTestHandler(&fakeReaderStore{}, &fakeAuthorCollectionStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/readers/nobody/collections", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": "nobody"})
	rec := httptest.NewRecorder()
	h.Collections(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждём 200", rec.Code)
	}
	if body := rec.Body.String(); body != "[]\n" && body != "[]" {
		t.Fatalf("пустой список приехал как %q, ждём []", body)
	}
}

func TestReaderAdminCollectionsFailureIsJSONMessage(t *testing.T) {
	collections := &fakeAuthorCollectionStore{failWith: errors.New("склад недоступен")}
	h := newReaderAdminTestHandler(&fakeReaderStore{}, collections)

	req := httptest.NewRequest(http.MethodGet, "/api/readers/chtec/collections", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": "chtec"})
	rec := httptest.NewRecorder()
	h.Collections(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, ждём 500", rec.Code)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Message == "" {
		t.Fatalf("в ответе нет поля message: %s", rec.Body.String())
	}
}

// Пустой ник в пути до склада доходить не должен: сравнение по пустой строке
// совпало бы с сотрудническими подборками (у них снимок ника пуст), то есть
// читательский разбор жалобы выдал бы витрину.
func TestReaderAdminCollectionsRejectsEmptyNickname(t *testing.T) {
	collections := &fakeAuthorCollectionStore{}
	h := newReaderAdminTestHandler(&fakeReaderStore{}, collections)

	req := httptest.NewRequest(http.MethodGet, "/api/readers//collections", nil)
	req = mux.SetURLVars(req, map[string]string{"nickname": "  "})
	rec := httptest.NewRecorder()
	h.Collections(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ждём 400 на пустой ник", rec.Code)
	}
	if collections.gotNickname != "" {
		t.Errorf("склад всё-таки вызвали с ником %q", collections.gotNickname)
	}
}
