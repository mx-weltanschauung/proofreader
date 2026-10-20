package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/internal/repository"
)

// fakeEventWriteStore — запись событий; поверх fakeEventStore (чтение).
type fakeEventWriteStore struct {
	fakeEventStore
	kinds       map[int64]*models.EventKind
	events      map[int64]*models.Event
	createdEv   *models.Event
	updatedEv   *models.Event
	createEvErr error
	agendaErr   error
	agendaIn    *models.AgendaInput
	deleteKErr  error
	nextID      int64
}

func (f *fakeEventWriteStore) CreateOrganization(_ context.Context, o *models.EventOrganization) error {
	if o.ID == 0 {
		f.nextID++
		o.ID = f.nextID
	}
	return nil
}
func (f *fakeEventWriteStore) UpdateOrganization(context.Context, *models.EventOrganization) error {
	return nil
}
func (f *fakeEventWriteStore) CreateKind(_ context.Context, k *models.EventKind) error {
	if k.ID == 0 {
		f.nextID++
		k.ID = f.nextID
	}
	return nil
}
func (f *fakeEventWriteStore) UpdateKind(context.Context, *models.EventKind) error { return nil }
func (f *fakeEventWriteStore) DeleteKind(context.Context, int64) error             { return f.deleteKErr }
func (f *fakeEventWriteStore) KindByID(_ context.Context, id int64) (*models.EventKind, error) {
	if k, ok := f.kinds[id]; ok {
		return k, nil
	}
	return nil, repository.ErrEventKindNotFound
}
func (f *fakeEventWriteStore) CreateEvent(_ context.Context, e *models.Event) error {
	if f.createEvErr != nil {
		return f.createEvErr
	}
	f.createdEv = e
	if e.ID == 0 {
		f.nextID++
		e.ID = f.nextID
	}
	return nil
}
func (f *fakeEventWriteStore) UpdateEvent(_ context.Context, e *models.Event) error {
	cp := *e
	f.updatedEv = &cp
	return nil
}
func (f *fakeEventWriteStore) DeleteEvent(context.Context, int64) error { return nil }
func (f *fakeEventWriteStore) EventByID(_ context.Context, id int64) (*models.Event, error) {
	if e, ok := f.events[id]; ok {
		cp := *e
		return &cp, nil
	}
	return nil, repository.ErrEventNotFound
}
func (f *fakeEventWriteStore) ReplaceAgenda(_ context.Context, _ int64, in models.AgendaInput) error {
	f.agendaIn = &in
	return f.agendaErr
}

func newWriteFake() *fakeEventWriteStore {
	return &fakeEventWriteStore{
		kinds: map[int64]*models.EventKind{
			1: {ID: 1, OrganizationID: 10, Title: "Съезды"},
			2: {ID: 2, OrganizationID: 10, Title: "Пленумы"},
			3: {ID: 3, OrganizationID: 11, Title: "Чужой вид"},
		},
		events: map[int64]*models.Event{
			7: {ID: 7, KindID: 1, Slug: "i-sezd", Title: "I съезд"},
		},
		fakeEventStore: fakeEventStore{detail: closedOnlyEvent()},
	}
}

func writeHandler(f *fakeEventWriteStore) *EventHandler {
	return NewEventHandler(f, &fakeClosedStore{works: map[int64]string{9: models.ClosedByWork}})
}

func TestCreateEventRejectsReservedSlug(t *testing.T) {
	for _, slug := range []string{"edit", "new", "Bad Slug", ""} {
		f := newWriteFake()
		body := fmt.Sprintf(`{"slug":%q,"title":"I съезд"}`, slug)
		rec := doPostVars(t, writeHandler(f).CreateEvent, "/x", body, map[string]string{"id": "1"}, editorClaims())
		if rec.Code != http.StatusBadRequest || f.createdEv != nil {
			t.Errorf("слаг %q: %d %s", slug, rec.Code, rec.Body)
		}
	}
	f := newWriteFake()
	rec := doPostVars(t, writeHandler(f).CreateEvent, "/x", `{"slug":"ii-sezd","title":"II съезд"}`, map[string]string{"id": "1"}, editorClaims())
	if rec.Code != http.StatusCreated || f.createdEv == nil || f.createdEv.KindID != 1 {
		t.Errorf("нормальный слаг: %d %s", rec.Code, rec.Body)
	}
}

func TestCreateEventEmptyTitleIs400(t *testing.T) {
	rec := doPostVars(t, writeHandler(newWriteFake()).CreateEvent, "/x", `{"slug":"a","title":"  "}`, map[string]string{"id": "1"}, editorClaims())
	if rec.Code != http.StatusBadRequest {
		t.Errorf("%d", rec.Code)
	}
}

func TestCreateEventExplicitIDTakenIs409(t *testing.T) {
	f := newWriteFake()
	f.createEvErr = fmt.Errorf("%w: events.id = 5", repository.ErrEventIDTaken)
	rec := doPostVars(t, writeHandler(f).CreateEvent, "/x", `{"id":5,"slug":"a","title":"I съезд"}`, map[string]string{"id": "1"}, editorClaims())
	if rec.Code != http.StatusConflict {
		t.Errorf("id: %d", rec.Code)
	}
	f.createEvErr = repository.ErrEventSlugTaken
	rec = doPostVars(t, writeHandler(f).CreateEvent, "/x", `{"slug":"a","title":"I съезд"}`, map[string]string{"id": "1"}, editorClaims())
	if rec.Code != http.StatusConflict {
		t.Errorf("слаг: %d", rec.Code)
	}
	f.createEvErr = repository.ErrEventKindNotFound
	rec = doPostVars(t, writeHandler(f).CreateEvent, "/x", `{"slug":"a","title":"I съезд"}`, map[string]string{"id": "99"}, editorClaims())
	if rec.Code != http.StatusNotFound {
		t.Errorf("вид: %d", rec.Code)
	}
}

func TestUpdateEventIgnoresSlug(t *testing.T) {
	f := newWriteFake()
	rec := doPutVars(t, writeHandler(f).UpdateEvent, "/x", `{"slug":"hacked","title":"I съезд, уточнено","dates":"1903"}`, map[string]string{"id": "7"}, editorClaims())
	if rec.Code != http.StatusOK || f.updatedEv == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if f.updatedEv.Slug != "i-sezd" || f.updatedEv.KindID != 1 || f.updatedEv.Title != "I съезд, уточнено" {
		t.Errorf("записано: %+v", f.updatedEv)
	}
	var got models.Event
	if json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Slug != "i-sezd" {
		t.Errorf("ответ: %s", rec.Body)
	}
}

func TestUpdateEventMovesToKindOfSameOrganization(t *testing.T) {
	f := newWriteFake()
	rec := doPutVars(t, writeHandler(f).UpdateEvent, "/x", `{"kind_id":2,"title":"I съезд"}`, map[string]string{"id": "7"}, editorClaims())
	if rec.Code != http.StatusOK || f.updatedEv.KindID != 2 {
		t.Errorf("%d %+v", rec.Code, f.updatedEv)
	}
}

func TestUpdateEventForeignKindIs400(t *testing.T) {
	for _, kind := range []int{3, 99} {
		f := newWriteFake()
		rec := doPutVars(t, writeHandler(f).UpdateEvent, "/x", fmt.Sprintf(`{"kind_id":%d,"title":"I съезд"}`, kind), map[string]string{"id": "7"}, editorClaims())
		if rec.Code != http.StatusBadRequest || f.updatedEv != nil {
			t.Errorf("вид %d: %d %s", kind, rec.Code, rec.Body)
		}
	}
	rec := doPutVars(t, writeHandler(newWriteFake()).UpdateEvent, "/x", `{"title":"x"}`, map[string]string{"id": "404"}, editorClaims())
	if rec.Code != http.StatusNotFound {
		t.Errorf("нет события: %d", rec.Code)
	}
}

func TestDeleteKindInUseIs409(t *testing.T) {
	f := newWriteFake()
	f.deleteKErr = repository.ErrEventKindInUse
	rec := doDeleteVars(t, writeHandler(f).DeleteKind, "/x", map[string]string{"id": "1"}, editorClaims())
	if rec.Code != http.StatusConflict {
		t.Errorf("%d", rec.Code)
	}
	f.deleteKErr = nil
	rec = doDeleteVars(t, writeHandler(f).DeleteKind, "/x", map[string]string{"id": "1"}, editorClaims())
	if rec.Code != http.StatusNoContent {
		t.Errorf("%d", rec.Code)
	}
}

func TestOrganizationAndKindWriteValidation(t *testing.T) {
	h := writeHandler(newWriteFake())
	if rec := doPostVars(t, h.CreateOrganization, "/x", `{"slug":"Org A","title":"Организация А"}`, nil, editorClaims()); rec.Code != http.StatusBadRequest {
		t.Errorf("слаг организации: %d", rec.Code)
	}
	if rec := doPostVars(t, h.CreateOrganization, "/x", `{"slug":"org-a","title":""}`, nil, editorClaims()); rec.Code != http.StatusBadRequest {
		t.Errorf("заглавие организации: %d", rec.Code)
	}
	if rec := doPostVars(t, h.CreateOrganization, "/x", `{"slug":"org-a","title":"Организация А"}`, nil, editorClaims()); rec.Code != http.StatusCreated {
		t.Errorf("организация: %d %s", rec.Code, rec.Body)
	}
	if rec := doPostVars(t, h.CreateKind, "/x", `{"title":" "}`, map[string]string{"id": "10"}, editorClaims()); rec.Code != http.StatusBadRequest {
		t.Errorf("заглавие вида: %d", rec.Code)
	}
	if rec := doPostVars(t, h.CreateKind, "/x", `{"title":"Съезды"}`, map[string]string{"id": "10"}, editorClaims()); rec.Code != http.StatusCreated {
		t.Errorf("вид: %d %s", rec.Code, rec.Body)
	}
	if rec := doPutVars(t, h.UpdateKind, "/x", `{"title":"Съезды и конгрессы"}`, map[string]string{"id": "1"}, editorClaims()); rec.Code != http.StatusOK {
		t.Errorf("правка вида: %d %s", rec.Code, rec.Body)
	}
}

func agendaBody(sections string) string { return `{"sections":` + sections + `}` }

func TestAgendaRejectsChapterOfAnotherWork(t *testing.T) {
	f := newWriteFake()
	f.agendaErr = fmt.Errorf("%w: пункт 1, строка 1, цель 1: глава не из этого тома", repository.ErrAgendaTarget)
	body := agendaBody(`[{"title":"П","entries":[{"label":"Доклад","targets":[{"kind":"chapter","work_id":1,"chapter_id":99}]}]}]`)
	rec := doPutVars(t, writeHandler(f).ReplaceAgenda, "/x", body, map[string]string{"id": "7"}, editorClaims())
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "пункт 1, строка 1, цель 1: глава не из этого тома") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "agenda target") {
		t.Errorf("служебный префикс в тексте: %s", rec.Body)
	}
}

func TestAgendaValidatesLimitsAndLabels(t *testing.T) {
	long := strings.Repeat("я", 201)
	many := func(n int, item string) string {
		return "[" + strings.TrimSuffix(strings.Repeat(item+",", n), ",") + "]"
	}
	sec := `{"title":"П","entries":[]}`
	ent := func(label string) string { return `{"title":"П","entries":[{"label":"` + label + `","targets":[]}]}` }
	cases := map[string]struct{ body, want string }{
		"101 пункт":            {many(101, sec), "пунктов не больше 100"},
		"пустое заглавие":      {`[` + sec + `,{"title":"  ","entries":[]}]`, "пункт 2"},
		"заглавие 201 руна":    {`[{"title":"` + long + `","entries":[]}]`, "пункт 1"},
		"пустая подпись":       {`[` + sec + `,{"title":"П","entries":[{"label":"Доклад","targets":[]},{"label":"Доклад","targets":[]},{"label":" ","targets":[]}]}]`, "пункт 2, строка 3"},
		"подпись 201 руна":     {`[` + ent(long) + `]`, "пункт 1, строка 1"},
		"201 строка":           {`[{"title":"П","entries":` + many(201, `{"label":"a","targets":[]}`) + `}]`, "пункт 1"},
		"51 цель":              {`[{"title":"П","entries":[{"label":"a","targets":` + many(51, `{"kind":"work","work_id":1}`) + `}]}]`, "пункт 1, строка 1"},
		"work с chapter_id":    {`[{"title":"П","entries":[{"label":"a","targets":[{"kind":"work","work_id":1,"chapter_id":5}]}]}]`, "пункт 1, строка 1, цель 1"},
		"chapter без chapter":  {`[{"title":"П","entries":[{"label":"a","targets":[{"kind":"chapter","work_id":1}]}]}]`, "пункт 1, строка 1, цель 1"},
		"неизвестный вид цели": {`[{"title":"П","entries":[{"label":"a","targets":[{"kind":"page","work_id":1}]}]}]`, "пункт 1, строка 1, цель 1"},
		"подпись цели 201":     {`[{"title":"П","entries":[{"label":"a","targets":[{"kind":"work","work_id":1,"label":"` + long + `"}]}]}]`, "пункт 1, строка 1, цель 1"},
	}
	for name, c := range cases {
		f := newWriteFake()
		rec := doPutVars(t, writeHandler(f).ReplaceAgenda, "/x", agendaBody(c.body), map[string]string{"id": "7"}, editorClaims())
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) || f.agendaIn != nil {
			t.Errorf("%s: %d %s (ждали %q)", name, rec.Code, rec.Body, c.want)
		}
	}
	// граница: ровно 200 рун, пустая подпись цели, 100 пунктов — проходят
	f := newWriteFake()
	ok200 := strings.Repeat("я", 200)
	body := agendaBody(`[{"title":"` + ok200 + `","entries":[{"label":"` + ok200 + `","targets":[{"kind":"work","work_id":1,"label":""},{"kind":"chapter","work_id":1,"chapter_id":5}]}]}]`)
	rec := doPutVars(t, writeHandler(f).ReplaceAgenda, "/x", body, map[string]string{"id": "7"}, editorClaims())
	if rec.Code != http.StatusOK || f.agendaIn == nil {
		t.Errorf("граница: %d %s", rec.Code, rec.Body)
	}
	f = newWriteFake()
	rec = doPutVars(t, writeHandler(f).ReplaceAgenda, "/x", agendaBody(many(100, sec)), map[string]string{"id": "7"}, editorClaims())
	if rec.Code != http.StatusOK {
		t.Errorf("100 пунктов: %d %s", rec.Code, rec.Body)
	}
}

func TestAgendaReturnsStaffViewWithClosedMarked(t *testing.T) {
	f := newWriteFake()
	rec := doPutVars(t, writeHandler(f).ReplaceAgenda, "/x", agendaBody(`[]`), map[string]string{"id": "7"}, editorClaims())
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var d models.EventDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || len(d.Sections) != 1 || !d.Sections[0].Entries[0].Targets[0].Closed {
		t.Errorf("сотрудническая выдача с пометкой Closed: %v %s", err, rec.Body)
	}
}

func TestAgendaUnknownEventIs404(t *testing.T) {
	rec := doPutVars(t, writeHandler(newWriteFake()).ReplaceAgenda, "/x", agendaBody(`[]`), map[string]string{"id": "404"}, editorClaims())
	if rec.Code != http.StatusNotFound {
		t.Errorf("%d", rec.Code)
	}
}

// Публичный GET /organizations/{slug}/{eventSlug} не должен затенять
// сотруднические POST/PUT с тем же числом сегментов.
func TestEventWriteRoutesNotShadowedByPublicRead(t *testing.T) {
	f := newWriteFake()
	rt := createTestRouter()
	r := NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		rt.cacheHandler, nil, rt.authService).WithEvents(writeHandler(f)).Setup()
	tok := editorToken(t)

	req := httptest.NewRequest("POST", "/api/organizations/5/kinds", strings.NewReader(`{"title":"Съезды"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("POST kinds: %d %s", rec.Code, rec.Body)
	}

	req = httptest.NewRequest("PUT", "/api/organizations/5", strings.NewReader(`{"title":"Организация А"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("PUT организации: %d %s", rec.Code, rec.Body)
	}

	f.orgs = closedOnlyOrgs()
	req = httptest.NewRequest("GET", "/api/organizations/org-a/i-sezd", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var d models.EventDetail
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &d) != nil || d.Slug != "i-sezd" {
		t.Errorf("GET события: %d %s", rec.Code, rec.Body)
	}
}
