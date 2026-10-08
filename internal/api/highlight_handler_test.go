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

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

type fakeHighlightStore struct {
	edition  *models.Edition
	list     []models.EditionHighlight
	replaced []models.HighlightInput
	calls    int
	err      error
}

func (f *fakeHighlightStore) GetByID(ctx context.Context, id int64) (*models.Edition, error) {
	if f.edition == nil || f.edition.ID != id {
		// Так отвечает настоящий EditionRepository.GetByID на пустой строке.
		return nil, errors.New("edition not found")
	}
	return f.edition, nil
}

func (f *fakeHighlightStore) ListHighlights(ctx context.Context, id int64) ([]models.EditionHighlight, error) {
	return f.list, nil
}

func (f *fakeHighlightStore) ReplaceHighlights(ctx context.Context, id int64, items []models.HighlightInput) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.replaced = items
	return nil
}

func putHighlights(t *testing.T, h *HighlightHandler, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/editions/"+id+"/highlights", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": id})
	rec := httptest.NewRecorder()
	h.Replace(rec, req)
	return rec
}

func messageOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело отказа не JSON {message}: %q", rec.Body.String())
	}
	return body["message"]
}

func TestHighlightsReplaceStoresTrimmedList(t *testing.T) {
	store := &fakeHighlightStore{edition: &models.Edition{ID: 4}}
	rec := putHighlights(t, NewHighlightHandler(store), "4",
		`[{"chapter_id": 7, "label": "  Что делать?  "}, {"chapter_id": 9, "label": "   "}]`)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	want := []models.HighlightInput{{ChapterID: 7, Label: "Что делать?"}, {ChapterID: 9, Label: ""}}
	if len(store.replaced) != 2 || store.replaced[0] != want[0] || store.replaced[1] != want[1] {
		t.Fatalf("в хранилище ушло %+v, ждали %+v", store.replaced, want)
	}
}

func TestHighlightsReplaceAcceptsEmptyList(t *testing.T) {
	store := &fakeHighlightStore{edition: &models.Edition{ID: 4}}
	rec := putHighlights(t, NewHighlightHandler(store), "4", `[]`)
	if rec.Code != http.StatusOK || store.calls != 1 || len(store.replaced) != 0 {
		t.Fatalf("пустой список не снял витрину: код %d, вызовов %d", rec.Code, store.calls)
	}
}

func TestHighlightsReplaceRejectsBadLists(t *testing.T) {
	cyr80 := strings.Repeat("ж", 80)
	cases := map[string]struct {
		body string
		ok   bool
	}{
		"девять пунктов": {body: `[` + strings.TrimSuffix(strings.Repeat(`{"chapter_id": 1},`, 9), ",") + `]`},
		"повтор главы":   {body: `[{"chapter_id": 3}, {"chapter_id": 3}]`},
		"81 знак":        {body: `[{"chapter_id": 3, "label": "` + cyr80 + `ж"}]`},
		"80 кириллицей":  {body: `[{"chapter_id": 3, "label": "` + cyr80 + `"}]`, ok: true},
		"нулевой id":     {body: `[{"chapter_id": 0}]`},
		"не массив":      {body: `{"chapter_id": 3}`},
		// null декодируется в nil-срез и молча снёс бы витрину; снять её можно только «[]».
		"null": {body: `null`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := &fakeHighlightStore{edition: &models.Edition{ID: 4}}
			rec := putHighlights(t, NewHighlightHandler(store), "4", tc.body)
			if tc.ok {
				if rec.Code != http.StatusOK {
					t.Fatalf("законный список отклонён: %d %s", rec.Code, rec.Body.String())
				}
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("код %d, ждали 400", rec.Code)
			}
			if messageOf(t, rec) == "" {
				t.Fatal("отказ без текста")
			}
			if store.calls != 0 {
				t.Fatal("негодный список дошёл до хранилища")
			}
		})
	}
}

func TestHighlightsReplaceForeignChapterIs400(t *testing.T) {
	store := &fakeHighlightStore{edition: &models.Edition{ID: 4}, err: repository.ErrHighlightForeignChapter}
	rec := putHighlights(t, NewHighlightHandler(store), "4", `[{"chapter_id": 3}]`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(messageOf(t, rec), "собрани") {
		t.Fatalf("код %d, текст %q", rec.Code, rec.Body.String())
	}
}

func TestHighlightsUnknownEditionIs404(t *testing.T) {
	store := &fakeHighlightStore{edition: &models.Edition{ID: 4}}
	h := NewHighlightHandler(store)
	if rec := putHighlights(t, h, "5", `[]`); rec.Code != http.StatusNotFound {
		t.Fatalf("PUT: код %d", rec.Code)
	}
	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/editions/5/highlights", nil), map[string]string{"id": "5"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET: код %d", rec.Code)
	}
}

func TestHighlightsGetReturnsListNotNull(t *testing.T) {
	store := &fakeHighlightStore{edition: &models.Edition{ID: 4}}
	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/editions/4/highlights", nil), map[string]string{"id": "4"})
	rec := httptest.NewRecorder()
	NewHighlightHandler(store).Get(rec, req)
	if rec.Code != http.StatusOK || !bytes.HasPrefix(bytes.TrimSpace(rec.Body.Bytes()), []byte("[")) {
		t.Fatalf("код %d, тело %q", rec.Code, rec.Body.String())
	}
}
