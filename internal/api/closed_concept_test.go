package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"proofreader/internal/seo"
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
	got, ok := visibleConcept(closedConceptFixture(), map[int64]bool{2: true}, nil, closedConceptVols, nil)
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
	got, ok := visibleConcept(c, nil, ClosedSet{60: true}, closedConceptVols, nil)
	if !ok || len(got.Articles) != 1 || got.Articles[0].ID != 1 {
		t.Fatalf("статья закрытой работы осталась: ok=%v %+v", ok, got)
	}
}

func TestVisibleConceptDropsReferenceIntoClosedVolume(t *testing.T) {
	got, ok := visibleConcept(closedConceptFixture(), nil, ClosedSet{50: true}, closedConceptVols, nil)
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
	if _, ok := visibleConcept(c, map[int64]bool{1: true, 2: true}, nil, closedConceptVols, nil); ok {
		t.Error("все статьи закрыты, а понятие осталось")
	}
	// Статья без текста, чьи адреса все в закрытом томе, ничего не несёт.
	c = &models.IndexConcept{ID: 2, Slug: "x", Articles: []*models.IndexArticle{
		{ID: 5, EditionID: 1, References: []*models.IndexReference{{ID: 1, VolumeNumber: 5, PageStart: 1, PageEnd: 1}}},
	}}
	if _, ok := visibleConcept(c, nil, ClosedSet{50: true}, closedConceptVols, nil); ok {
		t.Error("пустая оболочка статьи осталась")
	}
}

func TestVisibleConceptUntouchedWhenNothingRemoved(t *testing.T) {
	// Понятие без статей и понятие, которого закрытое не касается, — как были.
	empty := &models.IndexConcept{ID: 3, Slug: "e"}
	if got, ok := visibleConcept(empty, map[int64]bool{2: true}, ClosedSet{50: true}, nil, nil); !ok || got != empty {
		t.Errorf("пустое понятие тронуто: %v %v", got, ok)
	}
	c := closedConceptFixture()
	if got, ok := visibleConcept(c, nil, nil, closedConceptVols, nil); !ok || got != c {
		t.Errorf("nil-множества что-то скрыли: %v %v", got, ok)
	}
	// Редирект «см.»: ни адресов, ни текста — и это не потеря.
	redirect := &models.IndexConcept{ID: 4, Slug: "r", Articles: []*models.IndexArticle{{ID: 9, EditionID: 1, Kind: models.IndexConceptKindRedirect}}}
	if _, ok := visibleConcept(redirect, map[int64]bool{2: true}, nil, closedConceptVols, nil); !ok {
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
	store.hiddenSlugs = map[string]bool{"skrytoe": true}
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
	if _, err := idx.GetConceptBySlug(context.Background(), "g"); err == nil || !seo.IsNotFound(err) {
		t.Errorf("скрытое понятие должно распознаваться seo.IsNotFound, а не %v", err)
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
		hiddenSlugs:      map[string]bool{"skrytoe": true},
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

// Число обращений к указателю на PageConcepts с N обратными ссылками: скрытость
// узнаётся ОДНИМ запросом, а не понятием и картой томов на каждую ссылку.
func TestPageConceptsBacklinkLookupDoesNotFanOut(t *testing.T) {
	const n = 10
	open := closedConceptFixture()
	by := map[string]*models.IndexConcept{}
	var backlinks []models.ConceptBacklink
	for i := 0; i < n; i++ {
		slug := "s" + strings.Repeat("x", i)
		by[slug] = &models.IndexConcept{ID: int64(100 + i), Slug: slug, Articles: []*models.IndexArticle{{ID: int64(100 + i), EditionID: 1, ArticleMarkdown: "t", References: []*models.IndexReference{{ID: 1, VolumeNumber: 4, PageStart: 1, PageEnd: 1}}}}}
		backlinks = append(backlinks, models.ConceptBacklink{Slug: slug, Title: slug})
	}
	store := &fakeIndexStore{concept: open, conceptsBySlug: by, volumesByEdition: closedConceptVols, backlinks: backlinks}
	vol, ed := 4, int64(1)
	h := NewIndexHandler(store, &fakeWorkGetter{work: &models.Work{ID: 40, EditionID: &ed, VolumeNumber: &vol}}, &fakeEditionStore{},
		&fakePageGetter{page: &models.Page{ID: 5, WorkID: 40, PageNumber: 3}},
		&fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer()).
		WithClosed(&fakeClosedStore{editions: map[int64]bool{2: true}})
	req := conceptReq("/api/works/40/pages/5/concepts", "", false)
	req = mux.SetURLVars(req, map[string]string{"workId": "40", "pageId": "5"})
	rec := httptest.NewRecorder()
	h.PageConcepts(rec, req)
	t.Logf("N=%d: GetConceptBySlug=%d VolumeMap=%d HiddenConceptSlugs=%d", n, store.getCalls, store.volumeCalls, store.hiddenCalls)
	if store.getCalls != 0 || store.volumeCalls != 0 || store.hiddenCalls != 1 {
		t.Errorf("ждали 0 понятий, 0 карт томов и 1 запрос скрытых")
	}
}

func TestBlankArticleTextAgreesWithTrimSpace(t *testing.T) {
	for _, s := range []string{"", " ", "\n", "\t\r\n", " ", " 　", "\u0085", "\v\f"} {
		if !models.IsBlankArticleText(s) || strings.TrimSpace(s) != "" {
			t.Errorf("%q должен быть пустым и для IsBlankArticleText, и для TrimSpace", s)
		}
	}
	if models.IsBlankArticleText(" а ") {
		t.Error("текст с буквой не пуст")
	}
	// Статья из одного «\n» с адресами в закрытом томе — пустая оболочка и в Go.
	c := &models.IndexConcept{ID: 2, Slug: "x", Articles: []*models.IndexArticle{
		{ID: 5, EditionID: 1, ArticleMarkdown: "\n", References: []*models.IndexReference{{ID: 1, VolumeNumber: 5, PageStart: 1, PageEnd: 1}}},
	}}
	if _, ok := visibleConcept(c, nil, ClosedSet{50: true}, closedConceptVols, nil); ok {
		t.Error("статья из одного перевода строки считается текстом")
	}
}

func TestHiddenLookupErrorIsNotFailOpen(t *testing.T) {
	hidden := &models.IndexConcept{ID: 7, Slug: "skrytoe", Articles: []*models.IndexArticle{{ID: 70, EditionID: 2, ArticleMarkdown: "x"}}}
	open := closedConceptFixture()
	store := &fakeIndexStore{concept: open, volumesByEdition: closedConceptVols,
		conceptsBySlug: map[string]*models.IndexConcept{"skrytoe": hidden},
		hiddenErr:      errors.New("база недоступна"),
		backlinks:      []models.ConceptBacklink{{Slug: "skrytoe", Title: "Скрытое"}},
		links:          []repository.LinkWithTarget{{TargetSlug: "skrytoe"}},
	}
	vol, ed := 4, int64(1)
	h := NewIndexHandler(store, &fakeWorkGetter{work: &models.Work{ID: 40, EditionID: &ed, VolumeNumber: &vol}}, &fakeEditionStore{},
		&fakePageGetter{page: &models.Page{ID: 5, WorkID: 40, PageNumber: 3}},
		&fakeChapterTrees{}, newFakeFragmentStore(), markdown.NewRenderer()).
		WithClosed(&fakeClosedStore{editions: map[int64]bool{2: true}})

	req := conceptReq("/api/works/40/pages/5/concepts", "", false)
	req = mux.SetURLVars(req, map[string]string{"workId": "40", "pageId": "5"})
	rec := httptest.NewRecorder()
	h.PageConcepts(rec, req)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "Скрытое") {
		t.Errorf("PageConcepts: код %d, тело %q — ждали 500 без заглавия", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.GetConcept(rec, conceptReq("/api/concepts/trud", "trud", false))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GetConcept: код %d, ждали 500", rec.Code)
	}
	// Для токенless-канала ошибка тоже ошибка, а не пропущенная отсылка.
	idx := NewTokenlessIndex(store, &fakeClosedStore{editions: map[int64]bool{2: true}})
	if _, err := idx.ArticleLinks(context.Background(), 1); err == nil {
		t.Error("TokenlessIndex.ArticleLinks проглотил ошибку")
	}
}

func TestTokenlessArticleLinksOneQuery(t *testing.T) {
	store := &fakeIndexStore{hiddenSlugs: map[string]bool{"skrytoe": true},
		links: []repository.LinkWithTarget{{TargetSlug: "skrytoe"}, {TargetSlug: "a"}, {TargetSlug: "b"}}}
	idx := NewTokenlessIndex(store, &fakeClosedStore{})
	got, err := idx.ArticleLinks(context.Background(), 1)
	if err != nil || len(got) != 2 {
		t.Fatalf("ссылок %d, ошибка %v; ждали 2", len(got), err)
	}
	if store.hiddenCalls != 1 || store.getCalls != 0 || store.volumeCalls != 0 {
		t.Errorf("запросов: скрытых %d, понятий %d, карт %d; ждали 1/0/0", store.hiddenCalls, store.getCalls, store.volumeCalls)
	}
}

func TestHiddenConceptIsGoneForSEOCrawler(t *testing.T) {
	if !errors.Is(errConceptHidden, seo.ErrNotFound) || !seo.IsNotFound(errConceptHidden) {
		t.Fatalf("errConceptHidden не распознаётся как отсутствие: %v", errConceptHidden)
	}
}

// closedPagesOf — closedPages над готовым набором «работа:страница».
func closedPagesOf(pairs ...[2]int64) func(int64, int) bool {
	set := map[[2]int64]bool{}
	for _, p := range pairs {
		set[p] = true
	}
	return func(w int64, page int) bool { return set[[2]int64{w, int64(page)}] }
}

// Адрес, разрешающийся в закрытую полосу открытого тома, скрыт; соседний
// открытый остаётся. Печатный номер возвращается в номер полосы через
// page_offset тома, как в resolveReference.
func TestConceptAddressIntoClosedPageHidden(t *testing.T) {
	vols := map[int64][]models.VolumeLocation{
		1: {{VolumeNumber: 4, WorkID: 40, PageOffset: 2, MaxPage: 100}, {VolumeNumber: 5, WorkID: 50, MaxPage: 100}},
	}
	c := &models.IndexConcept{ID: 1, Slug: "trud", EditionIDs: []int64{1}, Articles: []*models.IndexArticle{{
		ID: 1, EditionID: 1, Kind: models.IndexConceptKindArticle,
		References: []*models.IndexReference{
			{ID: 11, VolumeNumber: 4, PageStart: 8, PageEnd: 9},   // полосы 6—7: 7 закрыта
			{ID: 12, VolumeNumber: 4, PageStart: 3, PageEnd: 3},   // полоса 1
			{ID: 13, VolumeNumber: 5, PageStart: 7, PageEnd: 7}}}, // другой том, та же полоса 7
	}}
	got, ok := visibleConcept(c, nil, nil, vols, closedPagesOf([2]int64{40, 7}))
	if !ok || len(got.Articles[0].References) != 2 {
		t.Fatalf("адреса: ok=%v %+v", ok, got)
	}
	for _, r := range got.Articles[0].References {
		if r.ID == 11 {
			t.Errorf("адрес в закрытую полосу остался: %+v", r)
		}
	}
	// Все адреса закрыты, текста нет — понятие уходит.
	c2 := &models.IndexConcept{ID: 2, Articles: []*models.IndexArticle{{
		ID: 2, EditionID: 1, References: []*models.IndexReference{{ID: 21, VolumeNumber: 4, PageStart: 9, PageEnd: 9}}}}}
	if _, ok := visibleConcept(c2, nil, nil, vols, closedPagesOf([2]int64{40, 7})); ok {
		t.Error("понятие из одних закрытых адресов осталось")
	}
}

// Статья, выросшая из полос закрытой главы работы-указателя, скрыта.
func TestConceptArticleOnClosedSourcePagesHidden(t *testing.T) {
	c := &models.IndexConcept{ID: 1, EditionIDs: []int64{1}, Articles: []*models.IndexArticle{
		{ID: 1, EditionID: 1, WorkID: i64(90), SourcePageStart: 3, SourcePageEnd: 5, ArticleMarkdown: "закрытая"},
		{ID: 2, EditionID: 1, WorkID: i64(90), SourcePageStart: 1, SourcePageEnd: 2, ArticleMarkdown: "открытая"},
		{ID: 3, EditionID: 1, WorkID: i64(90), ArticleMarkdown: "без источника"},
	}}
	got, ok := visibleConcept(c, nil, nil, nil, closedPagesOf([2]int64{90, 4}))
	if !ok || len(got.Articles) != 2 || got.Articles[0].ID != 2 || got.Articles[1].ID != 3 {
		t.Fatalf("статьи: ok=%v %+v", ok, got)
	}
}

// chapterClosedStore — хранилище закрытого, у которого закрыты только главы:
// closedView.filter обязан дойти до адресов, хотя закрытых работ и изданий нет.
type chapterClosedStore struct {
	spanStore
	works map[int64]bool
}

func (chapterClosedStore) ClosedBy(context.Context, int64) (string, error) { return "", nil }
func (chapterClosedStore) ClosedIDs(context.Context) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}
func (chapterClosedStore) ClosedEditionIDs(context.Context) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}
func (s chapterClosedStore) WorksWithClosedChapters(context.Context) (map[int64]bool, error) {
	return s.works, nil
}

type fixedVolumes map[int64][]models.VolumeLocation

func (f fixedVolumes) VolumeMap(_ context.Context, id int64) ([]models.VolumeLocation, error) {
	return f[id], nil
}

func TestClosedViewFilterHidesAddressInClosedChapter(t *testing.T) {
	store := chapterClosedStore{spanStore: spanStore{40: {{Start: 6, End: 8}}}, works: map[int64]bool{40: true}}
	v, err := fullClosedView(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if v.none() {
		t.Fatal("закрыта глава — вид не должен быть пустым")
	}
	got, ok, err := v.filter(context.Background(), fixedVolumes(closedConceptVols), closedConceptFixture())
	if err != nil || !ok {
		t.Fatalf("filter: %v %v", ok, err)
	}
	// Адрес 11 — т. 4, с. 3 (полоса 3, открыта); 12 — т. 5 (работа 50, без закрытых глав).
	if len(got.Articles[0].References) != 2 {
		t.Fatalf("открытые адреса не должны пострадать: %+v", got.Articles[0].References)
	}
	c := closedConceptFixture()
	c.Articles[0].References[0].PageStart, c.Articles[0].References[0].PageEnd = 7, 7
	got, _, err = v.filter(context.Background(), fixedVolumes(closedConceptVols), c)
	if err != nil || len(got.Articles[0].References) != 1 || got.Articles[0].References[0].ID != 12 {
		t.Fatalf("адрес в закрытую главу остался: %+v %v", got.Articles[0].References, err)
	}
}
