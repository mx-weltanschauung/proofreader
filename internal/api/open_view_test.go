package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/models"
)

// fakeOpenWorkSource — все работы по id; закрытость знает только хранилище
// закрытых (fakeClosedStore), а не строка: ClosedBy в строках пуст нарочно —
// обёртка обязана судить по множеству, а не по полю, которое заполняют не
// все запросы.
type fakeOpenWorkSource struct {
	byID     map[int64]*models.Work
	children map[int64][]*models.Work
	loose    []models.ShelfWork
}

func (f *fakeOpenWorkSource) GetByID(_ context.Context, id int64) (*models.Work, error) {
	if w, ok := f.byID[id]; ok {
		return w, nil
	}
	return nil, fmt.Errorf("work not found")
}

func (f *fakeOpenWorkSource) List(context.Context, int, int, *models.WorkStatus, *int64) ([]*models.Work, error) {
	out := []*models.Work{}
	for id := int64(1); id <= int64(len(f.byID)); id++ {
		out = append(out, f.byID[id])
	}
	return out, nil
}

func (f *fakeOpenWorkSource) ListChildren(_ context.Context, parent int64) ([]*models.Work, error) {
	return f.children[parent], nil
}

func (f *fakeOpenWorkSource) ListWithoutEdition(context.Context) ([]models.ShelfWork, error) {
	return f.loose, nil
}

func openWorksFixture() (*fakeOpenWorkSource, *fakeClosedStore) {
	works := &fakeOpenWorkSource{byID: map[int64]*models.Work{
		1: {ID: 1, Title: "Открытый"}, 2: {ID: 2, Title: "Закрытый"},
		3: {ID: 3, Title: "Листы открытого"}, 4: {ID: 4, Title: "Листы закрытого"},
	}}
	works.children = map[int64][]*models.Work{1: {works.byID[3], works.byID[4]}}
	works.loose = []models.ShelfWork{{ID: 1, Title: "Открытый"}, {ID: 2, Title: "Закрытый"}}
	return works, &fakeClosedStore{works: map[int64]string{2: models.ClosedByWork, 4: models.ClosedByParent}}
}

func TestOpenWorksHidesClosed(t *testing.T) {
	ctx := context.Background()
	works, closed := openWorksFixture()
	open := NewOpenWorks(works, closed)

	if w, err := open.GetByID(ctx, 1); err != nil || w.ID != 1 {
		t.Fatalf("открытая работа: %v %v", w, err)
	}
	// Ровно этот текст: на суффикс «not found» смотрят seo.isNotFound и
	// isWorkNotFound — иначе /seo ответит 500, а не 410.
	if w, err := open.GetByID(ctx, 2); err == nil || err.Error() != "work not found" || w != nil {
		t.Fatalf("закрытая работа: %v, ошибка %v", w, err)
	}

	list, err := open.List(ctx, 100, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ids := workIDs(list); fmt.Sprint(ids) != "[1 3]" {
		t.Errorf("List: %v, ждали [1 3]", ids)
	}
	kids, err := open.ListChildren(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ids := workIDs(kids); fmt.Sprint(ids) != "[3]" {
		t.Errorf("ListChildren: %v, ждали [3]", ids)
	}
	loose, err := open.ListWithoutEdition(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(loose) != 1 || loose[0].ID != 1 {
		t.Errorf("ListWithoutEdition: %v, ждали только 1", loose)
	}
}

// Сбой хранилища закрытых — отказ, а не «ничего не закрыто»: каналы без токена
// иначе отдали бы закрытое всем.
func TestOpenWorksFailsClosedOnStoreError(t *testing.T) {
	ctx := context.Background()
	works, closed := openWorksFixture()
	closed.err = errors.New("база легла")
	open := NewOpenWorks(works, closed)
	if _, err := open.GetByID(ctx, 1); err == nil {
		t.Error("GetByID при сбое хранилища без ошибки")
	}
	if _, err := open.List(ctx, 100, 0, nil, nil); err == nil {
		t.Error("List при сбое хранилища без ошибки")
	}
	if _, err := open.ListChildren(ctx, 1); err == nil {
		t.Error("ListChildren при сбое хранилища без ошибки")
	}
	if _, err := open.ListWithoutEdition(ctx); err == nil {
		t.Error("ListWithoutEdition при сбое хранилища без ошибки")
	}
}

func workIDs(ws []*models.Work) []int64 {
	out := []int64{}
	for _, w := range ws {
		out = append(out, w.ID)
	}
	return out
}

type fakeOpenEditionSource struct {
	byID       map[int64]*models.Edition
	works      map[int64][]*models.Work
	summaries  []*models.VolumeSummary
	highlights []models.EditionHighlight
}

func (f *fakeOpenEditionSource) GetByID(_ context.Context, id int64) (*models.Edition, error) {
	if e, ok := f.byID[id]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("edition not found")
}

func (f *fakeOpenEditionSource) List(context.Context) ([]*models.Edition, error) {
	return []*models.Edition{f.byID[10], f.byID[20]}, nil
}

func (f *fakeOpenEditionSource) ListWorks(_ context.Context, id int64) ([]*models.Work, error) {
	return f.works[id], nil
}

func (f *fakeOpenEditionSource) ListWorkSummaries(_ context.Context, id int64) ([]*models.VolumeSummary, error) {
	var out []*models.VolumeSummary
	for _, s := range f.summaries {
		if s.EditionID != nil && *s.EditionID == id {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeOpenEditionSource) ListAllWorkSummaries(context.Context) ([]*models.VolumeSummary, error) {
	return f.summaries, nil
}

func (f *fakeOpenEditionSource) ListAllHighlights(context.Context) ([]models.EditionHighlight, error) {
	return f.highlights, nil
}

func TestOpenEditionsHidesClosed(t *testing.T) {
	ctx := context.Background()
	open10, closed20 := int64(10), int64(20)
	editions := &fakeOpenEditionSource{
		byID: map[int64]*models.Edition{10: {ID: 10, Title: "Открытое"}, 20: {ID: 20, Title: "Закрытое"}},
		works: map[int64][]*models.Work{
			10: {{ID: 1, Title: "Открытый том"}, {ID: 2, Title: "Закрытый том открытого"}},
			20: {{ID: 5, Title: "Том закрытого"}},
		},
		summaries: []*models.VolumeSummary{
			{Work: models.Work{ID: 1, EditionID: &open10}},
			{Work: models.Work{ID: 2, EditionID: &open10}},
			{Work: models.Work{ID: 5, EditionID: &closed20}},
		},
		highlights: []models.EditionHighlight{
			{EditionID: 10, WorkID: 1, ChapterID: 100},
			{EditionID: 10, WorkID: 2, ChapterID: 200},
			{EditionID: 20, WorkID: 5, ChapterID: 500},
		},
	}
	// Тома закрытого собрания закрыты и множеством работ (closed_works их
	// включает); собрание само — множеством изданий.
	closed := &fakeClosedStore{
		works:    map[int64]string{2: models.ClosedByWork, 5: models.ClosedByEdition},
		editions: map[int64]bool{20: true},
	}
	open := NewOpenEditions(editions, closed)

	if e, err := open.GetByID(ctx, 10); err != nil || e.ID != 10 {
		t.Fatalf("открытое собрание: %v %v", e, err)
	}
	if e, err := open.GetByID(ctx, 20); err == nil || err.Error() != "edition not found" || e != nil {
		t.Fatalf("закрытое собрание: %v, ошибка %v", e, err)
	}
	list, err := open.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != 10 {
		t.Errorf("List: %v, ждали только 10", list)
	}
	ws, err := open.ListWorks(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ids := workIDs(ws); fmt.Sprint(ids) != "[1]" {
		t.Errorf("ListWorks: %v, ждали [1]", ids)
	}
	if ws, err := open.ListWorks(ctx, 20); err != nil || len(ws) != 0 {
		t.Errorf("ListWorks закрытого собрания: %v %v", ws, err)
	}
	sums, err := open.ListWorkSummaries(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0].ID != 1 {
		t.Errorf("ListWorkSummaries: %v, ждали только 1", sums)
	}
	all, err := open.ListAllWorkSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != 1 {
		t.Errorf("ListAllWorkSummaries: %v, ждали только 1", all)
	}
	hs, err := open.ListAllHighlights(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 1 || hs[0].ChapterID != 100 {
		t.Errorf("ListAllHighlights: %v, ждали только главу 100", hs)
	}
}

func TestOpenEditionsFailsClosedOnStoreError(t *testing.T) {
	ctx := context.Background()
	editions := &fakeOpenEditionSource{byID: map[int64]*models.Edition{10: {ID: 10}, 20: {ID: 20}}}
	open := NewOpenEditions(editions, &fakeClosedStore{err: errors.New("база легла")})
	if _, err := open.GetByID(ctx, 10); err == nil {
		t.Error("GetByID при сбое хранилища без ошибки")
	}
	if _, err := open.List(ctx); err == nil {
		t.Error("List при сбое хранилища без ошибки")
	}
	if _, err := open.ListWorks(ctx, 10); err == nil {
		t.Error("ListWorks при сбое хранилища без ошибки")
	}
	if _, err := open.ListWorkSummaries(ctx, 10); err == nil {
		t.Error("ListWorkSummaries при сбое хранилища без ошибки")
	}
	if _, err := open.ListAllWorkSummaries(ctx); err == nil {
		t.Error("ListAllWorkSummaries при сбое хранилища без ошибки")
	}
	if _, err := open.ListAllHighlights(ctx); err == nil {
		t.Error("ListAllHighlights при сбое хранилища без ошибки")
	}
}

// workStoreAsOpenSource дотягивает подделку WorkStore выгрузки до
// openWorkSource: выгрузке нужны только List и GetByID.
type workStoreAsOpenSource struct{ WorkStore }

func (workStoreAsOpenSource) ListChildren(context.Context, int64) ([]*models.Work, error) {
	return nil, nil
}

func (workStoreAsOpenSource) ListWithoutEdition(context.Context) ([]models.ShelfWork, error) {
	return nil, nil
}

// openDownloadFixture — полная выгрузка над фикстурой подборки и её открытый
// двойник: тот же источник, работы — через OpenWorks.
func openDownloadFixture(closed ClosedStore) (full, open *DownloadSource) {
	full = downloadCollectionFixture()
	o := *full
	o.works = NewOpenWorks(workStoreAsOpenSource{full.works}, closed)
	return full, &o
}

func downloadAs(h http.HandlerFunc, claims *auth.Claims, target string, vars map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = mux.SetURLVars(req, vars)
	rec := httptest.NewRecorder()
	withClaims(claims, h)(rec, req)
	return rec
}

// Выгрузку не-сотруднику собирает открытый источник: закрытый том — 404 даже
// мимо closedGate (маршрут без промежуточного слоя), сотруднику — файл.
func TestDownloadNonStaffGetsOpenSource(t *testing.T) {
	closed := &fakeClosedStore{works: map[int64]string{1: models.ClosedByWork}}
	full, open := openDownloadFixture(closed)
	h := NewDownloadHandler(full).WithOpen(open).WithClosed(closed)

	for _, c := range []struct {
		name string
		fn   http.HandlerFunc
		url  string
		vars map[string]string
	}{
		{"том", h.Work, "/api/works/1/download?format=md", map[string]string{"id": "1"}},
		{"глава", h.Chapter, "/api/works/1/chapters/10/download?format=md", map[string]string{"workId": "1", "id": "10"}},
	} {
		if rec := downloadAs(c.fn, nil, c.url, c.vars); rec.Code != http.StatusNotFound {
			t.Errorf("%s анониму: %d, ждали 404", c.name, rec.Code)
		}
		if rec := downloadAs(c.fn, readerClaimsForOpen(), c.url, c.vars); rec.Code != http.StatusNotFound {
			t.Errorf("%s читателю: %d, ждали 404", c.name, rec.Code)
		}
		if rec := downloadAs(c.fn, editorClaims(), c.url, c.vars); rec.Code != http.StatusOK {
			t.Errorf("%s сотруднику: %d, ждали 200", c.name, rec.Code)
		}
	}
}

func readerClaimsForOpen() *auth.Claims {
	return &auth.Claims{UserID: 77, Role: models.RoleReader, Nickname: "читатель"}
}

// ETag выгрузки подборки зависит от того, кто пришёл, и от закрытости пунктов:
// без этого сотрудник и аноним делили бы ETag (общий кэш отдал бы одному файл
// другого), а после открытия работы аноним получал бы 304 на файл с
// «недоступным источником».
func TestDownloadCollectionETagFollowsViewerAndClosed(t *testing.T) {
	closed := &fakeClosedStore{works: map[int64]string{1: models.ClosedByWork}}
	full, open := openDownloadFixture(closed)
	// Подборку правили позже любой полосы: CacheKey в обоих состояниях — её
	// UpdatedAt, и открытие работы его не двигает (полосы пункта старее).
	// Иначе ETag сменился бы от Modified полос и проверка ничего не значила.
	full.collections.(*fakeCollectionStoreForDownload).collection.UpdatedAt = time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	h := NewDownloadHandler(full).WithOpen(open).WithClosed(closed)
	get := func(claims *auth.Claims) *httptest.ResponseRecorder {
		rec := downloadAs(h.Collection, claims, "/api/collections/moi/download?format=md", map[string]string{"slug": "moi"})
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
		}
		return rec
	}

	anon := get(nil)
	staff := get(editorClaims())
	if anon.Header().Get("ETag") == staff.Header().Get("ETag") {
		t.Error("ETag анонима и сотрудника совпали при закрытом пункте")
	}
	if !strings.Contains(anon.Header().Get("Vary"), "Authorization") {
		t.Errorf("Vary = %q, ждали Authorization", anon.Header().Get("Vary"))
	}

	closed.works = nil // работу открыли
	reopened := get(nil)
	if reopened.Header().Get("ETag") == anon.Header().Get("ETag") {
		t.Error("ETag анонима не сменился после открытия работы — 304 на файл с недоступным пунктом")
	}
}

// Vary стоит и на отказе: 404 анониму и файл сотруднику по одному адресу.
func TestDownloadVaryOnRefusal(t *testing.T) {
	closed := &fakeClosedStore{works: map[int64]string{1: models.ClosedByWork}}
	full, open := openDownloadFixture(closed)
	h := NewDownloadHandler(full).WithOpen(open).WithClosed(closed)
	rec := downloadAs(h.Work, nil, "/api/works/1/download?format=md", map[string]string{"id": "1"})
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Header().Get("Vary"), "Authorization") {
		t.Errorf("код %d, Vary %q", rec.Code, rec.Header().Get("Vary"))
	}
}
