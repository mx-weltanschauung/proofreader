package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
)

// fakeFeedbackStore — склад в памяти для тестов обработчика без базы.
type fakeFeedbackStore struct {
	letters []*models.Feedback
	nextID  int64
	failAll bool
}

// CreateWithinLimit — тот же атомарный контракт, что у настоящего склада:
// считает совпадения по отметке в окне и отказывает, не записывая, если
// предел уже исчерпан. В тестах гонки не бывает (вызовы последовательны),
// поэтому подставной склад держит проверку и запись одним методом без блокировок.
func (f *fakeFeedbackStore) CreateWithinLimit(ctx context.Context, letter *models.Feedback, limit int, since time.Time) (bool, error) {
	if f.failAll {
		return false, errors.New("склад недоступен")
	}
	n := 0
	for _, l := range f.letters {
		if l.IPHash == letter.IPHash && l.CreatedAt.After(since) {
			n++
		}
	}
	if n >= limit {
		return false, nil
	}
	f.nextID++
	letter.ID = f.nextID
	letter.CreatedAt = time.Now()
	cp := *letter
	f.letters = append(f.letters, &cp)
	return true, nil
}

func (f *fakeFeedbackStore) List(ctx context.Context, handled *bool, limit int) ([]*models.Feedback, error) {
	out := make([]*models.Feedback, 0, len(f.letters))
	for i := len(f.letters) - 1; i >= 0; i-- {
		letter := f.letters[i]
		if handled != nil && (letter.HandledAt != nil) != *handled {
			continue
		}
		out = append(out, letter)
	}
	return out, nil
}

func (f *fakeFeedbackStore) CountUnread(ctx context.Context) (int, error) {
	n := 0
	for _, letter := range f.letters {
		if letter.HandledAt == nil {
			n++
		}
	}
	return n, nil
}

func (f *fakeFeedbackStore) SetHandled(ctx context.Context, id int64, handled bool) error {
	for _, letter := range f.letters {
		if letter.ID == id {
			if handled {
				now := time.Now()
				letter.HandledAt = &now
			} else {
				letter.HandledAt = nil
			}
			return nil
		}
	}
	return errors.New("feedback not found")
}

func (f *fakeFeedbackStore) Delete(ctx context.Context, id int64) error {
	for i, letter := range f.letters {
		if letter.ID == id {
			f.letters = append(f.letters[:i], f.letters[i+1:]...)
			return nil
		}
	}
	return errors.New("feedback not found")
}

// seed кладёт письмо в подставной склад мимо предела частоты: тестам списка,
// счётчика и пометки предел не интересен.
func (f *fakeFeedbackStore) seed(letter *models.Feedback) {
	f.nextID++
	letter.ID = f.nextID
	letter.CreatedAt = time.Now()
	cp := *letter
	f.letters = append(f.letters, &cp)
}

func postFeedback(t *testing.T, h *FeedbackHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:40000"
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	return rec
}

func TestFeedbackCreateStoresLetter(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)

	rec := postFeedback(t, h, `{"message":"  нашёл опечатку  ","contact":"@ivan","source_path":"/works/16/pages/412"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201; тело: %s", rec.Code, rec.Body.String())
	}
	if len(store.letters) != 1 {
		t.Fatalf("на складе %d писем, ожидалось 1", len(store.letters))
	}
	letter := store.letters[0]
	if letter.Message != "нашёл опечатку" {
		t.Errorf("сообщение %q", letter.Message)
	}
	if letter.SourcePath != "/works/16/pages/412" {
		t.Errorf("source_path %q", letter.SourcePath)
	}
	if letter.IPHash == "" || strings.Contains(letter.IPHash, "203.0.113.9") {
		t.Errorf("отметка адреса %q: пустая или содержит сам адрес", letter.IPHash)
	}
}

// Читальня не собирает персональных данных, и держится это на том, что
// принять контакт попросту нечем: у запроса нет такого поля, у письма — тоже.
// Присланный старым клиентом или ботом `contact` (он есть в письме выше)
// json.Decoder молча отбрасывает. Стоит кому-нибудь вернуть поле «ради
// удобства» — падает здесь, а не через год на проверке.
func TestFeedbackRequestCarriesNoContact(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(createFeedbackRequest{}),
		reflect.TypeOf(models.Feedback{}),
	} {
		for i := range typ.NumField() {
			name := strings.ToLower(typ.Field(i).Name)
			tag := strings.ToLower(string(typ.Field(i).Tag))
			if strings.Contains(name, "contact") || strings.Contains(tag, "contact") {
				t.Errorf("%s снова принимает контакт: поле %s", typ.Name(), typ.Field(i).Name)
			}
		}
	}
}

// Ловушка: отвечаем как при успехе и ничего не пишем. Код ошибки подсказал бы
// боту, что его раскусили, и следующая версия ловушку обойдёт.
func TestFeedbackHoneypotAnswersOKAndStoresNothing(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)

	rec := postFeedback(t, h, `{"message":"купите ковры","binding_ref":"http://ковры"}`)

	if rec.Code != http.StatusCreated {
		t.Errorf("код %d, ожидался 201 (ловушка не должна выдавать себя)", rec.Code)
	}
	if len(store.letters) != 0 {
		t.Errorf("ловушка пропустила письмо на склад: %+v", store.letters)
	}
}

func TestFeedbackRateLimit(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)

	for i := 0; i < feedbackRateLimit; i++ {
		if rec := postFeedback(t, h, `{"message":"письмо"}`); rec.Code != http.StatusCreated {
			t.Fatalf("письмо %d отбито кодом %d", i+1, rec.Code)
		}
	}

	rec := postFeedback(t, h, `{"message":"шестое"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("код %d, ожидался 429", rec.Code)
	}
	if len(store.letters) != feedbackRateLimit {
		t.Errorf("на складе %d писем, ожидалось %d", len(store.letters), feedbackRateLimit)
	}

	// Час прошёл — окно уехало, и следующее письмо снова принимается.
	h.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if rec := postFeedback(t, h, `{"message":"через час"}`); rec.Code != http.StatusCreated {
		t.Errorf("после сдвига окна код %d, ожидался 201", rec.Code)
	}
}

// Текст ошибки обязан доехать до читателя. utils/apiError.ts читает
// err.response.data.message объекта — text/plain от http.Error туда не
// доходит вообще, и читатель видит только запасную фразу.
func TestFeedbackErrorsAreJSONWithMessage(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)

	rec := postFeedback(t, h, `{"message":"   "}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type %q, ожидался application/json", ct)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело не разобралось как JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Message == "" {
		t.Error("в теле нет message — читатель увидит только запасную фразу")
	}
}

func TestFeedbackRejectsOversizedBody(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)

	huge := `{"message":"` + strings.Repeat("a", feedbackMaxBody+1) + `"}`
	rec := postFeedback(t, h, huge)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("код %d, ожидался 413", rec.Code)
	}
	if len(store.letters) != 0 {
		t.Error("переросшее тело всё-таки записалось")
	}
}

func TestFeedbackDropsForeignSourcePath(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)

	rec := postFeedback(t, h, `{"message":"письмо","source_path":"https://чужой.сайт/страница"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d, ожидался 201", rec.Code)
	}
	if store.letters[0].SourcePath != "" {
		t.Errorf("source_path %q, ожидалась пустая строка", store.letters[0].SourcePath)
	}
}

// Находка проверки: пустая отметка адреса раньше отключала предел частоты
// целиком (ветка `if ipHash != ""` пропускала письмо вообще без проверки).
// Безадресные запросы должны делить общий предел наравне с обычными.
func TestFeedbackEmptyAddressSharesRateLimit(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader(body))
		req.RemoteAddr = "" // clientIP вернёт "", hashIP тоже — адреса нет вовсе
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		return rec
	}

	for i := 0; i < feedbackRateLimit; i++ {
		if rec := post(`{"message":"письмо"}`); rec.Code != http.StatusCreated {
			t.Fatalf("письмо %d отбито кодом %d", i+1, rec.Code)
		}
	}

	rec := post(`{"message":"шестое"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("код %d, ожидался 429 — пустая отметка обязана делить общий предел", rec.Code)
	}
}

// Находка проверки: падение склада отдавало читателю текст внутренней
// ошибки как есть — тот выдавал детали реализации (какое хранилище, что
// именно недоступно). Обработчик обязан вернуть 500 с непустым сообщением,
// не пересказывая исходную ошибку склада.
func TestFeedbackStoreFailureDoesNotLeakInternalMessage(t *testing.T) {
	store := &fakeFeedbackStore{failAll: true}
	h := NewFeedbackHandler(store, "соль", false)

	rec := postFeedback(t, h, `{"message":"письмо"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, ожидался 500", rec.Code)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело не разобралось как JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Message == "" {
		t.Error("в теле нет message")
	}
	if strings.Contains(body.Message, "склад недоступен") {
		t.Errorf("текст внутренней ошибки склада утёк наружу: %q", body.Message)
	}
}

func TestFeedbackListFilterAndOrder(t *testing.T) {
	store := &fakeFeedbackStore{}
	h := NewFeedbackHandler(store, "соль", false)
	for _, msg := range []string{"первое", "второе"} {
		store.seed(&models.Feedback{Message: msg})
	}
	if err := store.SetHandled(context.Background(), 1, true); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest("GET", "/api/feedback?handled=false", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var out []models.Feedback
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("тело: %v (%s)", err, rec.Body.String())
	}
	if len(out) != 1 || out[0].Message != "второе" {
		t.Fatalf("фильтр handled=false вернул %+v", out)
	}
}

// Пустой список — [], а не null: у null нет ни .length, ни .map, и фронт
// падает на первом же обращении. Прочие списочные маршруты читальни этим
// дефектом больны; здесь его не заводим.
func TestFeedbackListEmptyIsArray(t *testing.T) {
	h := NewFeedbackHandler(&fakeFeedbackStore{}, "соль", false)

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest("GET", "/api/feedback", nil))

	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("тело %q, ожидалось []", body)
	}
}

// Отметка адреса — служебная величина, наружу ей ходить незачем.
func TestFeedbackListHidesIPHash(t *testing.T) {
	store := &fakeFeedbackStore{}
	store.seed(&models.Feedback{Message: "письмо", IPHash: "секрет"})
	h := NewFeedbackHandler(store, "соль", false)

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest("GET", "/api/feedback", nil))

	if strings.Contains(rec.Body.String(), "секрет") {
		t.Errorf("отметка адреса ушла наружу: %s", rec.Body.String())
	}
}

func TestFeedbackUnreadCount(t *testing.T) {
	store := &fakeFeedbackStore{}
	for i := 0; i < 3; i++ {
		store.seed(&models.Feedback{Message: "письмо"})
	}
	if err := store.SetHandled(context.Background(), 2, true); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	h := NewFeedbackHandler(store, "соль", false)

	rec := httptest.NewRecorder()
	h.UnreadCount(rec, httptest.NewRequest("GET", "/api/feedback/unread-count", nil))

	var out struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("тело: %v", err)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, ожидалось 2", out.Count)
	}
}

func TestFeedbackDeleteAnswers204(t *testing.T) {
	store := &fakeFeedbackStore{}
	store.seed(&models.Feedback{Message: "письмо"})
	h := NewFeedbackHandler(store, "соль", false)

	req := httptest.NewRequest("DELETE", "/api/feedback/1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d, ожидался 204", rec.Code)
	}
	if len(store.letters) != 0 {
		t.Error("письмо осталось на складе")
	}
}

func TestFeedbackSetHandledAndDeleteMissing(t *testing.T) {
	h := NewFeedbackHandler(&fakeFeedbackStore{}, "соль", false)

	req := httptest.NewRequest("PATCH", "/api/feedback/77", strings.NewReader(`{"handled":true}`))
	req = mux.SetURLVars(req, map[string]string{"id": "77"})
	rec := httptest.NewRecorder()
	h.SetHandled(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("пометка несуществующего: код %d, ожидался 404", rec.Code)
	}

	req = httptest.NewRequest("DELETE", "/api/feedback/77", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "77"})
	rec = httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("удаление несуществующего: код %d, ожидался 404", rec.Code)
	}

	req = httptest.NewRequest("PATCH", "/api/feedback/abc", strings.NewReader(`{"handled":true}`))
	req = mux.SetURLVars(req, map[string]string{"id": "abc"})
	rec = httptest.NewRecorder()
	h.SetHandled(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("нечисловой id: код %d, ожидался 400", rec.Code)
	}
}
