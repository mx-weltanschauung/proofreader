package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

// Нумерация томов собрания (миграция 000039): печатная по умолчанию,
// условная — только явным словом редактора, и правка без поля её не снимает.

func TestEditionCreateDefaultsToPrintedNumbering(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/editions",
		bytes.NewBufferString(`{"title":"Сочинения","slug":"s"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	if store.created.VolumeNumbering != models.VolumeNumberingPrinted {
		t.Fatalf("VolumeNumbering = %q, ожидалось printed", store.created.VolumeNumbering)
	}
}

func TestEditionCreateKeepsOrderNumbering(t *testing.T) {
	store := &fakeEditionStore{}
	h := NewEditionHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/api/editions",
		bytes.NewBufferString(`{"title":"Стенограммы","slug":"ki","volume_numbering":"order"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
	}
	if store.created.VolumeNumbering != models.VolumeNumberingOrder {
		t.Fatalf("VolumeNumbering = %q, ожидалось order", store.created.VolumeNumbering)
	}
}

func TestEditionRejectsUnknownNumbering(t *testing.T) {
	h := NewEditionHandler(&fakeEditionStore{})
	req := httptest.NewRequest(http.MethodPost, "/api/editions",
		bytes.NewBufferString(`{"title":"С","slug":"s","volume_numbering":"roman"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("создание: код %d, ожидался 400", rec.Code)
	}

	store := &fakeEditionStore{editions: []*models.Edition{{ID: 7, Title: "С", Slug: "s",
		VolumeNumbering: models.VolumeNumberingOrder}}}
	h = NewEditionHandler(store)
	req = httptest.NewRequest(http.MethodPut, "/api/editions/7",
		bytes.NewBufferString(`{"title":"С","slug":"s","volume_numbering":""}`))
	req = mux.SetURLVars(req, map[string]string{"id": "7"})
	rec = httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("правка: код %d, ожидался 400", rec.Code)
	}
	if store.editions[0].VolumeNumbering != models.VolumeNumberingOrder {
		t.Fatalf("отказ всё же переписал нумерацию: %q", store.editions[0].VolumeNumbering)
	}
}

func TestEditionUpdateNumbering(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"без поля — прежняя", `{"title":"С","slug":"s"}`, models.VolumeNumberingOrder},
		{"явно печатная", `{"title":"С","slug":"s","volume_numbering":"printed"}`, models.VolumeNumberingPrinted},
		{"явно условная", `{"title":"С","slug":"s","volume_numbering":"order"}`, models.VolumeNumberingOrder},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &fakeEditionStore{editions: []*models.Edition{{ID: 7, Title: "С", Slug: "s",
				VolumeNumbering: models.VolumeNumberingOrder}}}
			h := NewEditionHandler(store)
			req := httptest.NewRequest(http.MethodPut, "/api/editions/7", bytes.NewBufferString(c.body))
			req = mux.SetURLVars(req, map[string]string{"id": "7"})
			rec := httptest.NewRecorder()
			h.Update(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("код %d; тело: %s", rec.Code, rec.Body.String())
			}
			if got := store.editions[0].VolumeNumbering; got != c.want {
				t.Fatalf("VolumeNumbering = %q, ожидалось %q", got, c.want)
			}
		})
	}
}

// Подпись тома на титуле выгрузки, в OPDS и в isPartOf краулера: у условной
// нумерации её нет вовсе, у печатной — как была.
func TestVolumeLabelHidesOrderOnlyNumber(t *testing.T) {
	n, part := 24, "II"
	printed := &models.Work{VolumeNumber: &n, VolumePart: &part}
	if got := volumeLabel(printed); got != "Том 24, II" {
		t.Fatalf("печатная нумерация: %q, ожидалось «Том 24, II»", got)
	}
	order := &models.Work{VolumeNumber: &n, VolumePart: &part, VolumeOrderOnly: true}
	if got := volumeLabel(order); got != "" {
		t.Fatalf("условная нумерация: %q, ожидалась пустая подпись", got)
	}
}

// Указатель адресует тома печатным номером — собранию с условной нумерацией
// его не ввезти: иначе страница понятия печатала бы выдуманное «т. 24».
func TestImportRefusesOrderOnlyEdition(t *testing.T) {
	for _, c := range []struct {
		numbering string
		want      int
	}{
		{models.VolumeNumberingOrder, http.StatusConflict},
		{models.VolumeNumberingPrinted, http.StatusOK},
	} {
		store := &fakeIndexStore{}
		editions := &fakeEditionStore{editions: []*models.Edition{{ID: 9, VolumeNumbering: c.numbering}}}
		h := NewIndexHandler(store, &fakeWorkGetter{}, editions, &fakePageGetter{}, &fakeChapterTrees{},
			newFakeFragmentStore(), markdown.NewRenderer())
		req := httptest.NewRequest(http.MethodPost, "/api/editions/9/index/import",
			bytes.NewBufferString(`{"concepts":[{"title":"Труд","slug":"trud","sort_key":"труд","kind":"article"}]}`))
		req = mux.SetURLVars(req, map[string]string{"id": "9"})
		rec := httptest.NewRecorder()
		h.Import(rec, req)
		if rec.Code != c.want {
			t.Fatalf("%s: код %d, ожидался %d; тело: %s", c.numbering, rec.Code, c.want, rec.Body.String())
		}
		if got := store.replacedIn == 9; got != (c.want == http.StatusOK) {
			t.Fatalf("%s: ввоз дошёл до хранилища = %v", c.numbering, got)
		}
	}
}
