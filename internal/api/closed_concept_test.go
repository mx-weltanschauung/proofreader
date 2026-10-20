package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

func i64(v int64) *int64 { return &v }

// closedConceptFixture: статья 1 — издание 1 (открыто), адреса в т. 4 (работа
// 40, открыта) и т. 5 (работа 50, закрыта); статья 2 — издание 2 (закрыто).
func closedConceptFixture() *models.IndexConcept {
	return &models.IndexConcept{
		ID: 1, Title: "Труд", Slug: "trud", EditionIDs: []int64{1, 2},
		Articles: []*models.IndexArticle{
			{ID: 1, EditionID: 1, Kind: models.IndexConceptKindArticle, ArticleMarkdown: "Текст.",
				References: []*models.IndexReference{
					{ID: 11, VolumeNumber: 4, PageStart: 3, PageEnd: 3},
					{ID: 12, VolumeNumber: 5, PageStart: 9, PageEnd: 9},
				}},
			{ID: 2, EditionID: 2, Kind: models.IndexConceptKindArticle, ArticleMarkdown: "Закрытая статья."},
		},
	}
}

var closedConceptVols = map[int64][]models.VolumeLocation{
	1: {{VolumeNumber: 4, WorkID: 40, MaxPage: 100}, {VolumeNumber: 5, WorkID: 50, MaxPage: 100}},
	2: {{VolumeNumber: 1, WorkID: 60, MaxPage: 100}},
}

func TestVisibleConceptDropsClosedEditionArticle(t *testing.T) {
	got, ok := visibleConcept(closedConceptFixture(), map[int64]bool{2: true}, nil, closedConceptVols)
	if !ok || len(got.Articles) != 1 || got.Articles[0].ID != 1 {
		t.Fatalf("статья закрытого издания осталась: ok=%v %+v", ok, got)
	}
	if len(got.EditionIDs) != 1 || got.EditionIDs[0] != 1 {
		t.Errorf("id закрытого издания остался в агрегате: %v", got.EditionIDs)
	}
}

func TestVisibleConceptDropsArticleOfClosedWork(t *testing.T) {
	c := closedConceptFixture()
	c.Articles[1].EditionID = 1
	c.Articles[1].WorkID = i64(60)
	got, ok := visibleConcept(c, nil, ClosedSet{60: true}, closedConceptVols)
	if !ok || len(got.Articles) != 1 || got.Articles[0].ID != 1 {
		t.Fatalf("статья закрытой работы осталась: ok=%v %+v", ok, got)
	}
}

func TestVisibleConceptDropsReferenceIntoClosedVolume(t *testing.T) {
	got, ok := visibleConcept(closedConceptFixture(), nil, ClosedSet{50: true}, closedConceptVols)
	if !ok {
		t.Fatal("понятие исчезло, хотя видимое осталось")
	}
	refs := got.Articles[0].References
	if len(refs) != 1 || refs[0].ID != 11 {
		t.Fatalf("адрес в закрытый том остался: %+v", refs)
	}
	if len(got.Articles) != 2 {
		t.Errorf("статья открытого издания не должна уходить: %d", len(got.Articles))
	}
}

func TestVisibleConceptAllClosedIsGone(t *testing.T) {
	c := closedConceptFixture()
	if _, ok := visibleConcept(c, map[int64]bool{1: true, 2: true}, nil, closedConceptVols); ok {
		t.Error("все статьи закрыты, а понятие осталось")
	}
	// Статья без текста, чьи адреса все в закрытом томе, ничего не несёт.
	c = &models.IndexConcept{ID: 2, Slug: "x", Articles: []*models.IndexArticle{
		{ID: 5, EditionID: 1, References: []*models.IndexReference{{ID: 1, VolumeNumber: 5, PageStart: 1, PageEnd: 1}}},
	}}
	if _, ok := visibleConcept(c, nil, ClosedSet{50: true}, closedConceptVols); ok {
		t.Error("пустая оболочка статьи осталась")
	}
}

func TestVisibleConceptUntouchedWhenNothingRemoved(t *testing.T) {
	// Понятие без статей и понятие, которого закрытое не касается, — как были.
	empty := &models.IndexConcept{ID: 3, Slug: "e"}
	if got, ok := visibleConcept(empty, map[int64]bool{2: true}, ClosedSet{50: true}, nil); !ok || got != empty {
		t.Errorf("пустое понятие тронуто: %v %v", got, ok)
	}
	c := closedConceptFixture()
	if got, ok := visibleConcept(c, nil, nil, closedConceptVols); !ok || got != c {
		t.Errorf("nil-множества что-то скрыли: %v %v", got, ok)
	}
	// Редирект «см.»: ни адресов, ни текста — и это не потеря.
	redirect := &models.IndexConcept{ID: 4, Slug: "r", Articles: []*models.IndexArticle{{ID: 9, EditionID: 1, Kind: models.IndexConceptKindRedirect}}}
	if _, ok := visibleConcept(redirect, map[int64]bool{2: true}, nil, closedConceptVols); !ok {
		t.Error("редирект без адресов скрыт без причины")
	}
}

func conceptReq(path, slug string, staff bool) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = mux.SetURLVars(req, map[string]string{"slug": slug})
	if staff {
		ctx := context.WithValue(req.Context(), middleware.UserContextKey, &auth.Claims{Role: models.RoleEditor})
		req = req.WithContext(ctx)
	}
	return req
}

func closedConceptHandler(c *models.IndexConcept, closed *fakeClosedStore) (*IndexHandler, *fakeIndexStore) {
	store := &fakeIndexStore{concept: c, volumesByEdition: closedConceptVols}
	h := NewIndexHandler(store, &fakeWorkGetter{}, &fakeEditionStore{}, &fakePageGetter{}, &fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer()).WithClosed(closed)
	return h, store
}

func TestConceptOfClosedEditionIs404ForAnonymous(t *testing.T) {
	c := closedConceptFixture()
	c.Articles = c.Articles[1:] // остаётся только статья закрытого издания 2
	h, _ := closedConceptHandler(c, &fakeClosedStore{editions: map[int64]bool{2: true}})

	rec := httptest.NewRecorder()
	h.GetConcept(rec, conceptReq("/api/concepts/trud", "trud", false))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("аноним: код %d, ждали 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.GetConcept(rec, conceptReq("/api/concepts/trud", "trud", true))
	if rec.Code != http.StatusOK {
		t.Fatalf("сотрудник: код %d, ждали 200", rec.Code)
	}
}

func TestGetConceptHidesClosedAddressesAndLinks(t *testing.T) {
	c := closedConceptFixture()
	hiddenTarget := &models.IndexConcept{ID: 7, Slug: "skrytoe", Title: "Скрытое", Articles: []*models.IndexArticle{{ID: 70, EditionID: 2, ArticleMarkdown: "x"}}}
	h, store := closedConceptHandler(c, &fakeClosedStore{
		works: map[int64]string{50: models.ClosedByWork}, editions: map[int64]bool{2: true}})
	store.conceptsBySlug = map[string]*models.IndexConcept{"trud": c, "skrytoe": hiddenTarget}
	store.links = []repository.LinkWithTarget{{TargetSlug: "skrytoe"}, {TargetSlug: "trud"}}
	store.incoming = []repository.IncomingLink{{Slug: "skrytoe", Title: "Скрытое"}, {Slug: "trud", Title: "Труд"}}

	rec := httptest.NewRecorder()
	h.GetConcept(rec, conceptReq("/api/concepts/trud", "trud", false))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"Закрытая статья", `"page_start":9`, "skrytoe", "Скрытое"} {
		if strings.Contains(body, leak) {
			t.Errorf("в ответе анониму осталось %q: %s", leak, body)
		}
	}
	var got struct {
		Articles []struct {
			References []struct {
				PageStart int `json:"page_start"`
			} `json:"references"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Articles) != 1 || len(got.Articles[0].References) != 1 || got.Articles[0].References[0].PageStart != 3 {
		t.Errorf("ждали одну статью с одним адресом: %s", body)
	}

	rec = httptest.NewRecorder()
	h.GetConcept(rec, conceptReq("/api/concepts/trud", "trud", true))
	if !strings.Contains(rec.Body.String(), "Закрытая статья") || !strings.Contains(rec.Body.String(), `"page_start":9`) {
		t.Errorf("сотрудник не увидел закрытое: %s", rec.Body.String())
	}
}

func TestListConceptsAsksRepositoryToHideClosedForAnonymous(t *testing.T) {
	h, store := closedConceptHandler(closedConceptFixture(), &fakeClosedStore{})
	for staff, want := range map[bool]bool{false: false, true: true} {
		store.listClosedArg = nil
		rec := httptest.NewRecorder()
		h.ListConcepts(rec, conceptReq("/api/concepts", "", staff))
		if store.listClosedArg == nil || *store.listClosedArg != want {
			t.Errorf("сотрудник=%v: includeClosed=%v, ждали %v", staff, store.listClosedArg, want)
		}
	}
}

func TestExpandPageOfClosedWorkIs404(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"}}}
	pages.page = &models.Page{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "текст"}
	for _, staff := range []bool{false, true} {
		h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)
		h.WithClosed(&fakeClosedStore{works: map[int64]string{14: models.ClosedByWork}})
		req := conceptReq("/api/concepts/x/references/473/pages/9143", "abstraktnyj-trud", staff)
		req = mux.SetURLVars(req, map[string]string{"slug": "abstraktnyj-trud", "refId": "473", "pageId": "9143"})
		rec := httptest.NewRecorder()
		h.ExpandPage(rec, req)
		want := http.StatusNotFound
		if staff {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("сотрудник=%v: код %d, ждали %d: %s", staff, rec.Code, want, rec.Body.String())
		}
	}
}

func TestFragmentsOfClosedWorkReturnNothing(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: "секрет"}}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)
	h.WithClosed(&fakeClosedStore{works: map[int64]string{14: models.ClosedByWork}})
	rec := httptest.NewRecorder()
	h.Fragments(rec, conceptReq("/api/concepts/x/fragments", "abstraktnyj-trud", false))
	if strings.Contains(rec.Body.String(), "секрет") || strings.Contains(rec.Body.String(), `"work_id":14`) {
		t.Errorf("поток отдал закрытое: %s", rec.Body.String())
	}
}

func TestTokenlessIndexHidesClosedAlways(t *testing.T) {
	c := closedConceptFixture()
	store := &fakeIndexStore{concept: c, volumesByEdition: closedConceptVols}
	idx := NewTokenlessIndex(store, &fakeClosedStore{
		works: map[int64]string{50: models.ClosedByWork}, editions: map[int64]bool{2: true}})
	got, err := idx.GetConceptBySlug(context.Background(), "trud")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Articles) != 1 || len(got.Articles[0].References) != 1 {
		t.Fatalf("токенless-канал увидел закрытое: %+v", got.Articles)
	}
	if _, err := idx.ListConcepts(context.Background(), "", "", 10, 0); err != nil {
		t.Fatal(err)
	}
	if store.listClosedArg == nil || *store.listClosedArg {
		t.Error("ListConcepts каналу без токена должен идти с includeClosed=false")
	}
	// Все статьи закрыты — отвечает как отсутствующее (seo.isNotFound по суффиксу).
	c2 := &models.IndexConcept{ID: 2, Slug: "g", Articles: []*models.IndexArticle{{ID: 1, EditionID: 2, ArticleMarkdown: "x"}}}
	store.concept = c2
	if _, err := idx.GetConceptBySlug(context.Background(), "g"); err == nil || !strings.HasSuffix(err.Error(), "not found") {
		t.Errorf("скрытое понятие должно отвечать «not found», а не %v", err)
	}
}

func TestPageConceptsDropsHiddenConceptTitle(t *testing.T) {
	hidden := &models.IndexConcept{ID: 7, Slug: "skrytoe", Title: "Скрытое",
		Articles: []*models.IndexArticle{{ID: 70, EditionID: 2, ArticleMarkdown: "x"}}}
	open := closedConceptFixture()
	store := &fakeIndexStore{
		concept:          open,
		conceptsBySlug:   map[string]*models.IndexConcept{"skrytoe": hidden, "trud": open},
		volumesByEdition: closedConceptVols,
		backlinks: []models.ConceptBacklink{
			{Slug: "skrytoe", Title: "Скрытое"}, {Slug: "trud", Title: "Труд"},
		},
	}
	vol, ed := 4, int64(1)
	work := &models.Work{ID: 40, EditionID: &ed, VolumeNumber: &vol}
	page := &models.Page{ID: 5, WorkID: 40, PageNumber: 3}
	h := NewIndexHandler(store, &fakeWorkGetter{work: work}, &fakeEditionStore{}, &fakePageGetter{page: page},
		&fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer()).
		WithClosed(&fakeClosedStore{editions: map[int64]bool{2: true}})
	for staff, wantHidden := range map[bool]bool{false: false, true: true} {
		req := conceptReq("/api/works/40/pages/5/concepts", "", staff)
		req = mux.SetURLVars(req, map[string]string{"workId": "40", "pageId": "5"})
		rec := httptest.NewRecorder()
		h.PageConcepts(rec, req)
		if got := strings.Contains(rec.Body.String(), "Скрытое"); got != wantHidden {
			t.Errorf("сотрудник=%v: заглавие скрытого понятия в ответе=%v, ждали %v: %s", staff, got, wantHidden, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Труд") {
			t.Errorf("видимое понятие пропало: %s", rec.Body.String())
		}
	}
}
