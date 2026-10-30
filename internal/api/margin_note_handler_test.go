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

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// fakeMarginNotes — хранилище заметок в памяти; ошибки той же формы, что у
// репозитория (суффикс «not found»).
type fakeMarginNotes struct {
	created           []models.MarginNoteInput
	createErr         error
	notes             map[int64]*models.MarginNote // id -> заметка (OwnerID внутри)
	listOwner         int64
	listIncludeClosed bool
}

func (f *fakeMarginNotes) Create(_ context.Context, in models.MarginNoteInput) (*models.MarginNote, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, in)
	return &models.MarginNote{ID: 1, WorkID: in.WorkID, Target: in.Target, Body: in.Body}, nil
}

func (f *fakeMarginNotes) own(ownerID, id int64) (*models.MarginNote, error) {
	n, ok := f.notes[id]
	if !ok || n.OwnerID != ownerID {
		return nil, fmt.Errorf("margin note not found")
	}
	return n, nil
}

func (f *fakeMarginNotes) UpdateBody(_ context.Context, ownerID, id int64, body string, _ bool) (*models.MarginNote, error) {
	n, err := f.own(ownerID, id)
	if err != nil {
		return nil, err
	}
	n.Body = body
	return n, nil
}

func (f *fakeMarginNotes) Delete(_ context.Context, ownerID, id int64, _ bool) error {
	_, err := f.own(ownerID, id)
	return err
}

func (f *fakeMarginNotes) ListByWork(_ context.Context, ownerID, _ int64, includeClosed bool) ([]*models.MarginNote, error) {
	f.listOwner, f.listIncludeClosed = ownerID, includeClosed
	return []*models.MarginNote{}, nil
}

func (f *fakeMarginNotes) ListByOwner(_ context.Context, ownerID int64, _ *int64, _, _ int, includeClosed bool) ([]*models.MarginNote, int, error) {
	f.listOwner, f.listIncludeClosed = ownerID, includeClosed
	return []*models.MarginNote{}, 0, nil
}

type fakeMarginWorks struct{ ids map[int64]bool }

func (f fakeMarginWorks) List(context.Context, int, int, *models.WorkStatus, *int64) ([]*models.Work, error) {
	return nil, nil
}

func (f fakeMarginWorks) GetByID(_ context.Context, id int64) (*models.Work, error) {
	if !f.ids[id] {
		return nil, fmt.Errorf("work not found")
	}
	return &models.Work{ID: id}, nil
}

// fakeMarginPages: открыты только перечисленные номера; закрытая полоса
// отвечает так же, как обёртка чтения, — «not found».
type fakeMarginPages struct{ open map[int]int64 } // номер -> id

func (f fakeMarginPages) GetByWorkAndPageNumber(_ context.Context, workID int64, n int) (*models.Page, error) {
	id, ok := f.open[n]
	if !ok {
		return nil, fmt.Errorf("page not found")
	}
	return &models.Page{ID: id, WorkID: workID, PageNumber: n}, nil
}

type fakeMarginChapters struct{ byID map[int64]*models.Chapter }

func (f fakeMarginChapters) GetByID(_ context.Context, id int64) (*models.Chapter, error) {
	c, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("chapter not found")
	}
	return c, nil
}

func marginRig(claims *auth.Claims) (*fakeMarginNotes, http.Handler) {
	notes := &fakeMarginNotes{notes: map[int64]*models.MarginNote{
		9: {ID: 9, OwnerID: 5, Body: "моя"},
	}}
	h := NewMarginNoteHandler(notes,
		fakeMarginWorks{ids: map[int64]bool{7: true, 8: true}},
		fakeMarginPages{open: map[int]int64{24: 240, 25: 250}},
		fakeMarginChapters{byID: map[int64]*models.Chapter{
			10: {ID: 10, WorkID: 7, Title: "Глава"},
			11: {ID: 11, WorkID: 8, Title: "Чужая"},
			12: {ID: 12, WorkID: 7, Title: ""},
		}})
	r := mux.NewRouter()
	r.HandleFunc("/api/works/{workId}/margin-notes", withClaims(claims, h.Create)).Methods("POST")
	r.HandleFunc("/api/works/{workId}/margin-notes", withClaims(claims, h.ListByWork)).Methods("GET")
	r.HandleFunc("/api/margin-notes", withClaims(claims, h.List)).Methods("GET")
	r.HandleFunc("/api/margin-notes/{id}", withClaims(claims, h.Update)).Methods("PUT")
	r.HandleFunc("/api/margin-notes/{id}", withClaims(claims, h.Delete)).Methods("DELETE")
	return notes, r
}

func marginDo(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMarginNoteCreateQuote(t *testing.T) {
	notes, h := marginRig(testReaderClaims)
	rec := marginDo(h, "POST", "/api/works/7/margin-notes",
		`{"target":"quote","start_page":24,"end_page":25,"quote_key":"Без","quote_tail":"конец","quote_text":"текст","body":"  мысль  "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	if len(notes.created) != 1 {
		t.Fatal("заметка не записана")
	}
	in := notes.created[0]
	if in.Target != models.MarginTargetQuote || in.OwnerID != 5 || in.WorkID != 7 || in.Body != "мысль" {
		t.Errorf("вход: %+v", in)
	}
	if in.StartPageID == nil || *in.StartPageID != 240 || in.EndPageID == nil || *in.EndPageID != 250 {
		t.Errorf("id полос не найдены по номерам: %+v", in)
	}
	if *in.StartPageNumber != 24 || *in.EndPageNumber != 25 {
		t.Errorf("номера полос: %+v", in)
	}
}

func TestMarginNoteCreateChapterTakesTitleFromServer(t *testing.T) {
	notes, h := marginRig(testReaderClaims)
	rec := marginDo(h, "POST", "/api/works/7/margin-notes", `{"target":"chapter","chapter_id":10,"body":"x"}`)
	if rec.Code != http.StatusCreated || notes.created[0].ChapterTitle != "Глава" || *notes.created[0].ChapterID != 10 {
		t.Fatalf("код %d, вход %+v", rec.Code, notes.created)
	}
}

// Заглавие главы бывает пустым, а снимок обязан быть непустым (CHECK
// margin_notes_chapter_shape) — без подстановки вставка падала бы 500.
func TestMarginNoteCreateChapterWithoutTitle(t *testing.T) {
	notes, h := marginRig(testReaderClaims)
	rec := marginDo(h, "POST", "/api/works/7/margin-notes", `{"target":"chapter","chapter_id":12,"body":"x"}`)
	if rec.Code != http.StatusCreated || len(notes.created) != 1 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	if got := notes.created[0].ChapterTitle; got != "Глава без заглавия" {
		t.Fatalf("снимок заглавия: %q", got)
	}
}

// Тело сверх потолка отвергается до хранилища: лишнее поле JSON в сотни
// килобайт иначе читалось бы целиком и проходило в запись.
func TestMarginNoteOversizeBodyRejected(t *testing.T) {
	junk := strings.Repeat("ж", 100<<10)
	notes, h := marginRig(testReaderClaims)
	rec := marginDo(h, "POST", "/api/works/7/margin-notes", `{"target":"work","body":"x","junk":"`+junk+`"}`)
	if rec.Code != http.StatusRequestEntityTooLarge || len(notes.created) != 0 {
		t.Fatalf("POST: код %d, записано %d", rec.Code, len(notes.created))
	}
	rec = marginDo(h, "PUT", "/api/margin-notes/9", `{"body":"новая","junk":"`+junk+`"}`)
	if rec.Code != http.StatusRequestEntityTooLarge || notes.notes[9].Body != "моя" {
		t.Fatalf("PUT: код %d, текст %q", rec.Code, notes.notes[9].Body)
	}

	// Самая длинная законная заметка в четырёхбайтовых знаках проходит.
	full := func(n int) string { return strings.Repeat("😀", n) }
	body, _ := json.Marshal(map[string]any{
		"target": "quote", "start_page": 24, "end_page": 24,
		"quote_key": full(models.MarginNoteQuoteMaxRunes), "quote_tail": full(models.MarginNoteQuoteMaxRunes),
		"quote_text": full(models.MarginNoteQuoteMaxRunes), "body": full(models.MarginNoteBodyMaxRunes),
	})
	if rec := marginDo(h, "POST", "/api/works/7/margin-notes", string(body)); rec.Code != http.StatusCreated {
		t.Fatalf("законная заметка в %d байт: код %d: %s", len(body), rec.Code, rec.Body)
	}
}

func TestMarginNoteCreateValidation(t *testing.T) {
	long := strings.Repeat("я", 4001)
	cases := []struct {
		name, path, body string
		code             int
		msg              string
	}{
		{"пусто", "/api/works/7/margin-notes", `{"target":"work","body":"  "}`, 400, "Заметка пуста"},
		{"длинная", "/api/works/7/margin-notes", `{"target":"work","body":"` + strings.Repeat("я", 10001) + `"}`, 400, "Заметка длиннее 10 000 знаков"},
		{"вид", "/api/works/7/margin-notes", `{"target":"zzz","body":"x"}`, 400, "Неизвестный вид заметки"},
		{"конец раньше начала", "/api/works/7/margin-notes", `{"target":"quote","start_page":25,"end_page":24,"quote_key":"k","body":"x"}`, 400, "Не указано место цитаты"},
		{"пустой ключ", "/api/works/7/margin-notes", `{"target":"quote","start_page":24,"end_page":24,"quote_key":" ","body":"x"}`, 400, "Не указано место цитаты"},
		{"длинный ключ", "/api/works/7/margin-notes", `{"target":"quote","start_page":24,"end_page":24,"quote_key":"` + long + `","body":"x"}`, 400, "Цитата слишком длинная"},
		{"длинный хвост", "/api/works/7/margin-notes", `{"target":"quote","start_page":24,"end_page":24,"quote_key":"k","quote_tail":"` + long + `","body":"x"}`, 400, "Цитата слишком длинная"},
		{"длинный текст", "/api/works/7/margin-notes", `{"target":"quote","start_page":24,"end_page":24,"quote_key":"k","quote_text":"` + long + `","body":"x"}`, 400, "Цитата слишком длинная"},
		{"нет полосы", "/api/works/7/margin-notes", `{"target":"quote","start_page":99,"end_page":99,"quote_key":"k","body":"x"}`, 404, "Не найдено"},
		{"нет главы", "/api/works/7/margin-notes", `{"target":"chapter","chapter_id":404,"body":"x"}`, 404, "Не найдено"},
		{"нет тома", "/api/works/99/margin-notes", `{"target":"work","body":"x"}`, 404, "Не найдено"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			notes, h := marginRig(testReaderClaims)
			rec := marginDo(h, "POST", c.path, c.body)
			if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.msg) {
				t.Errorf("код %d, тело %s; ждали %d %q", rec.Code, rec.Body, c.code, c.msg)
			}
			if len(notes.created) != 0 {
				t.Error("отказ дошёл до хранилища")
			}
		})
	}
}

func TestMarginNoteCreateLimit(t *testing.T) {
	notes, h := marginRig(testReaderClaims)
	notes.createErr = repository.ErrMarginNoteLimit
	rec := marginDo(h, "POST", "/api/works/7/margin-notes", `{"target":"work","body":"x"}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "Слишком много заметок: удалите старые") {
		t.Errorf("код %d: %s", rec.Code, rec.Body)
	}
}

func TestMarginNoteCreateChapterOfOtherWork(t *testing.T) {
	notes, h := marginRig(testReaderClaims)
	rec := marginDo(h, "POST", "/api/works/7/margin-notes", `{"target":"chapter","chapter_id":11,"body":"x"}`)
	if rec.Code != 404 || len(notes.created) != 0 {
		t.Errorf("код %d, записано %d", rec.Code, len(notes.created))
	}
}

// Закрытая полоса: обёртка чтения отвечает «not found» — хранилище не зовётся.
func TestMarginNoteCreateClosedPage(t *testing.T) {
	notes, h := marginRig(testReaderClaims)
	rec := marginDo(h, "POST", "/api/works/7/margin-notes",
		`{"target":"quote","start_page":24,"end_page":26,"quote_key":"k","body":"x"}`) // 26 «закрыта»
	if rec.Code != 404 || len(notes.created) != 0 {
		t.Errorf("код %d, записано %d", rec.Code, len(notes.created))
	}
}

func TestMarginNoteCreateWithoutClaims(t *testing.T) {
	_, h := marginRig(nil)
	if rec := marginDo(h, "POST", "/api/works/7/margin-notes", `{"target":"work","body":"x"}`); rec.Code != 401 {
		t.Errorf("код %d", rec.Code)
	}
}

func TestMarginNoteListUsesClaimsOwner(t *testing.T) {
	for _, c := range []struct {
		claims *auth.Claims
		staff  bool
	}{
		{testReaderClaims, false},
		{&auth.Claims{UserID: 5, Role: models.RoleEditor}, true},
	} {
		for _, path := range []string{"/api/margin-notes?owner_id=99", "/api/works/7/margin-notes?owner_id=99"} {
			notes, h := marginRig(c.claims)
			if rec := marginDo(h, "GET", path, ""); rec.Code != 200 {
				t.Fatalf("%s: код %d", path, rec.Code)
			}
			if notes.listOwner != 5 || notes.listIncludeClosed != c.staff {
				t.Errorf("%s роль %s: owner=%d includeClosed=%v", path, c.claims.Role, notes.listOwner, notes.listIncludeClosed)
			}
		}
	}
}

func TestMarginNoteUpdateOwn(t *testing.T) {
	_, h := marginRig(testReaderClaims)
	rec := marginDo(h, "PUT", "/api/margin-notes/9", `{"body":" новая "}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"body":"новая"`) {
		t.Errorf("код %d: %s", rec.Code, rec.Body)
	}
	if rec := marginDo(h, "PUT", "/api/margin-notes/9", `{"body":""}`); rec.Code != 400 {
		t.Errorf("пустой текст: код %d", rec.Code)
	}
}

func TestMarginNoteUpdateForeignIs404(t *testing.T) {
	_, h := marginRig(&auth.Claims{UserID: 6, Role: models.RoleReader})
	if rec := marginDo(h, "PUT", "/api/margin-notes/9", `{"body":"x"}`); rec.Code != 404 {
		t.Errorf("код %d", rec.Code)
	}
}

func TestMarginNoteDeleteForeignIs404(t *testing.T) {
	_, h := marginRig(&auth.Claims{UserID: 6, Role: models.RoleReader})
	if rec := marginDo(h, "DELETE", "/api/margin-notes/9", ""); rec.Code != 404 {
		t.Errorf("код %d", rec.Code)
	}
	_, h = marginRig(testReaderClaims)
	if rec := marginDo(h, "DELETE", "/api/margin-notes/9", ""); rec.Code != 204 {
		t.Errorf("свою: код %d", rec.Code)
	}
}

// Заметки тома едут голым массивом (контракт фронта), пустой — «[]», не null.
func TestMarginNoteListByWorkIsBareArray(t *testing.T) {
	_, h := marginRig(testReaderClaims)
	rec := marginDo(h, "GET", "/api/works/7/margin-notes", "")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("код %d, тело %q", rec.Code, rec.Body)
	}
	var got []models.MarginNote
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
}
