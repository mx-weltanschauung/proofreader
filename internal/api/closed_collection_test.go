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
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
)

const closedTitle = "Закрытая глава"

// closedItemRow — элемент подборки на главе закрытой работы 41; снимок и
// живые заглавия несут узнаваемую строку, по которой ловится утечка.
func closedItemRow() repository.ItemRow {
	row := chapterRow(11, 1, 100, 41)
	row.Item.SnapshotTitle = closedTitle
	row.ChapterTitle = ptrStr(closedTitle)
	row.WorkTitle = ptrStr("Закрытый том")
	return row
}

func closedWorks41() *fakeClosedStore {
	return &fakeClosedStore{works: map[int64]string{41: models.ClosedByWork}}
}

func TestCollectionTOCClosedItemHasNoTitle(t *testing.T) {
	// Источник пропал вовсе (WorkTitle == nil) и стоит на закрытой работе:
	// без проверки закрытости первым делом строка вышла бы broken со снимком.
	row := closedItemRow()
	row.WorkTitle = nil
	row.ChapterTitle = nil
	entries := buildCollectionTOC([]repository.ItemRow{row}, nil, ClosedSet{41: true})

	raw, _ := json.Marshal(entries)
	if strings.Contains(string(raw), closedTitle) || strings.Contains(string(raw), "снимок") {
		t.Fatalf("снимок заглавия закрытого уехал в JSON: %s", raw)
	}
	e := entries[0]
	if !e.Unavailable || e.Broken || e.Title != "" || e.Source != nil || len(e.Children) != 0 {
		t.Fatalf("пункт закрытой работы собран неверно: %+v", e)
	}
	if !strings.Contains(string(raw), `"unavailable":true`) {
		t.Fatalf("нет признака unavailable: %s", raw)
	}
}

func TestCollectionTOCOpenItemNotUnavailable(t *testing.T) {
	entries := buildCollectionTOC([]repository.ItemRow{chapterRow(1, 1, 100, 41)}, nil, ClosedSet{99: true})
	raw, _ := json.Marshal(entries)
	if entries[0].Unavailable || strings.Contains(string(raw), `"unavailable"`) {
		t.Fatalf("открытый пункт помечен недоступным: %s", raw)
	}
}

func closedCollectionHandler(store *fakeCollectionStore, closed *fakeClosedStore) *CollectionHandler {
	h := NewCollectionHandler(store, &fakePageStore{}, markdown.NewRenderer(), NewRangeCache(pagecache.New("")), "", false)
	return h.WithClosed(closed)
}

func closedCollectionStore() *fakeCollectionStore {
	store := collectionFixture()
	store.rows = []repository.ItemRow{closedItemRow()}
	store.item = &models.CollectionItem{
		ID: 11, CollectionID: 1, Kind: models.CollectionItemKindChapter,
		ChapterID: ptrInt64(100), WorkID: ptrInt64(41), SnapshotTitle: closedTitle,
	}
	return store
}

func TestCollectionGetClosedItemHasNoTitle(t *testing.T) {
	h := closedCollectionHandler(closedCollectionStore(), closedWorks41())
	req := httptest.NewRequest(http.MethodGet, "/api/collections/materializm", nil)
	req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})

	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if strings.Contains(rec.Body.String(), "Людвиг Фейербах") || strings.Contains(rec.Body.String(), "Закрытый том") ||
		strings.Contains(rec.Body.String(), closedTitle) ||
		!strings.Contains(rec.Body.String(), `"unavailable":true`) {
		t.Fatalf("закрытое в ответе подборки: %s", rec.Body.String())
	}

	// Сотрудник видит всё, как раньше.
	rec = httptest.NewRecorder()
	withClaims(editorClaims(), h.Get)(rec, req)
	if !strings.Contains(rec.Body.String(), "Людвиг Фейербах") || !strings.Contains(rec.Body.String(), "Закрытый том") ||
		strings.Contains(rec.Body.String(), `"unavailable"`) {
		t.Fatalf("сотруднику закрытое не показано: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"unavailable"`) {
		t.Fatalf("сотруднику пункт помечен недоступным: %s", rec.Body.String())
	}
}

func TestCollectionItemPagesClosedIs410(t *testing.T) {
	store := closedCollectionStore()
	h := closedCollectionHandler(store, closedWorks41())

	// Диапазон прогрет сотрудником: не-сотруднику файл из кэша не уходит.
	staffRec := httptest.NewRecorder()
	withClaims(editorClaims(), h.ItemPages)(staffRec, itemPagesRequestFor("materializm", "11"))
	if staffRec.Code != http.StatusOK {
		t.Fatalf("сотрудник: код %d, ожидался 200: %s", staffRec.Code, staffRec.Body.String())
	}

	rec := httptest.NewRecorder()
	h.ItemPages(rec, itemPagesRequestFor("materializm", "11"))
	if rec.Code != http.StatusGone {
		t.Fatalf("код %d, ожидался 410 (после прогрева кэша сотрудником); тело: %s", rec.Code, rec.Body.String())
	}
}

func itemPagesRequestFor(slug, id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/collections/"+slug+"/items/"+id+"/pages", nil)
	return mux.SetURLVars(req, map[string]string{"slug": slug, "itemId": id})
}

func TestCollectionAddItemClosedTargetIs404ForReader(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	newStore := func() *fakeCollectionStore {
		s := collectionFixture()
		s.collections[0].OwnerID = &owner
		s.collections[0].AuthorNickname = testCollectionOwnerClaims.Nickname
		s.chapterWork = map[int64]int64{100: 41}
		return s
	}
	for name, body := range map[string]string{
		"глава": `{"kind":"chapter","chapter_id":100}`,
		"том":   `{"kind":"work","work_id":41}`,
	} {
		store := newStore()
		h := closedCollectionHandler(store, closedWorks41())
		req := httptest.NewRequest(http.MethodPost, "/api/collections/хранитель/materializm/items", strings.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"nickname": owner2nick(), "slug": "materializm"})
		rec := httptest.NewRecorder()
		withClaims(testCollectionOwnerClaims, h.AddItem)(rec, req)
		if rec.Code != http.StatusNotFound || store.addedKind != "" {
			t.Fatalf("%s: код %d, добавлено %q; ожидался 404 без записи", name, rec.Code, store.addedKind)
		}

		// Сотруднику добавлять можно.
		store = newStore()
		store.collections[0].OwnerID = nil
		store.collections[0].AuthorNickname = ""
		h = closedCollectionHandler(store, closedWorks41())
		req = httptest.NewRequest(http.MethodPost, "/api/collections/materializm/items", strings.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"slug": "materializm"})
		rec = httptest.NewRecorder()
		withClaims(editorClaims(), h.AddItem)(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: сотрудник получил %d: %s", name, rec.Code, rec.Body.String())
		}
	}
}

// Сбой ChapterWorkID не пропускает проверку закрытости: 500 или 404, записи нет.
func TestCollectionAddItemChapterLookupFailsClosed(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"сбой базы": {errors.New("connection reset"), http.StatusInternalServerError},
		"главы нет": {errors.New("chapter 100 not found"), http.StatusNotFound},
	} {
		store := collectionFixture()
		store.collections[0].OwnerID = &owner
		store.collections[0].AuthorNickname = testCollectionOwnerClaims.Nickname
		store.chapterWorkErr = tc.err
		h := closedCollectionHandler(store, closedWorks41())
		// work_id присылается заведомо открытым: запасной ход на него пропустил бы закрытую главу.
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"kind":"chapter","chapter_id":100,"work_id":5}`))
		req = mux.SetURLVars(req, map[string]string{"nickname": owner2nick(), "slug": "materializm"})
		rec := httptest.NewRecorder()
		withClaims(testCollectionOwnerClaims, h.AddItem)(rec, req)
		if rec.Code != tc.want || store.addedKind != "" {
			t.Fatalf("%s: код %d, добавлено %q; ожидался %d без записи", name, rec.Code, store.addedKind, tc.want)
		}
	}
}

func owner2nick() string { return testCollectionOwnerClaims.Nickname }

func TestCollectionDownloadMissingHasNoTitle(t *testing.T) {
	src := downloadCollectionFixture()
	store := src.collections.(*fakeCollectionStoreForDownload)
	store.rows[1].Item.WorkID = int64Ptr(41)
	store.rows[1].Item.SnapshotTitle = closedTitle
	store.rows[0].Item.WorkID = int64Ptr(1)

	b, err := src.Collection(context.Background(), "", "moi", ClosedSet{41: true})
	if err != nil {
		t.Fatalf("Collection: %v", err)
	}
	if len(b.Meta.Missing) != 1 || b.Meta.Missing[0] != "недоступный источник" {
		t.Fatalf("Missing = %q, ожидалась одна строка «недоступный источник»", b.Meta.Missing)
	}
	for _, s := range b.Sections {
		if strings.Contains(s.Title, closedTitle) {
			t.Fatalf("заглавие закрытого в секциях: %q", s.Title)
		}
	}

	// Без закрытости тот же пункт — прежнее поведение: подпись по снимку.
	b, err = src.Collection(context.Background(), "", "moi", nil)
	if err != nil || len(b.Meta.Missing) != 1 || b.Meta.Missing[0] == "недоступный источник" {
		t.Fatalf("прежнее поведение сломано: %v %q", err, b.Meta.Missing)
	}
}

func TestSEOCollectionSourceClosedIsTokenless(t *testing.T) {
	store := closedCollectionStore()
	src := NewSEOCollectionSource(store).WithClosed(closedWorks41())
	c, err := src.GetBySlug(context.Background(), "materializm")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "Людвиг Фейербах") || strings.Contains(string(raw), "Закрытый том") ||
		!strings.Contains(string(raw), `"unavailable":true`) {
		t.Fatalf("краулеру отдано закрытое: %s", raw)
	}
}

func TestDocumentCutOfClosedWorkUnavailable(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(41),
		Anchor: models.Anchor{StartPageID: 10, EndPageID: 10, EndOffset: 5},
		Status: models.CutStatusOK, SourceTitle: "Секретный источник",
	}
	for _, status := range []string{models.CutStatusOK, models.CutStatusStale} {
		cut.Status = status
		html := assembleDocument(markdown.NewRenderer(), `<cut id="7">`,
			[]*models.DocumentCut{cut}, map[int64][]*models.Page{}, map[int64]int{}, map[int64]bool{7: true})
		want := `<p class="document-cut document-cut--unavailable" data-cut-id="7">Вклейка недоступна.</p>`
		if !strings.Contains(html, want) || strings.Contains(html, "Секретный источник") {
			t.Fatalf("статус %s: вклейка закрытой работы: %s", status, html)
		}
	}
}

func TestAssembleDocumentForClosedCut(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(41), DocumentID: 3,
		Anchor: models.Anchor{StartPageID: 10, EndPageID: 10, EndOffset: 5},
		Status: models.CutStatusOK, SourceTitle: "Секретный источник",
	}
	cuts := newFakeDocumentCutStore(cut)
	cuts.pagesByCutID = map[int64][]*models.Page{7: {cutPage(10, 4, "Текст закрытой работы")}}
	doc := &models.Document{ID: 3, MarkdownContent: `<cut id="7">`}

	html, _, err := assembleDocumentFor(context.Background(), markdown.NewRenderer(), cuts, &fakeWorkStore{}, doc, ClosedSet{41: true})
	if err != nil || !strings.Contains(html, "document-cut--unavailable") ||
		strings.Contains(html, "Секретный источник") || strings.Contains(html, "Текст закрытой") {
		t.Fatalf("err=%v html=%s", err, html)
	}
	if cuts.pagesOfCutCalls != 0 {
		t.Fatalf("полосы закрытой вклейки читались %d раз", cuts.pagesOfCutCalls)
	}

	works := &fakeWorkStore{getFn: func(context.Context, int64) (*models.Work, error) { return &models.Work{ID: 41}, nil }}
	html, _, _ = assembleDocumentFor(context.Background(), markdown.NewRenderer(), cuts, works, doc, nil)
	if strings.Contains(html, "document-cut--unavailable") || !strings.Contains(html, "document-cut-body") {
		t.Fatalf("при пустом множестве вклейка должна собираться: %s", html)
	}
}

func TestSEODocumentSourceClosedCutIsTokenless(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(41), DocumentID: 3,
		Anchor: models.Anchor{StartPageID: 10, EndPageID: 10, EndOffset: 5},
		Status: models.CutStatusOK, SourceTitle: "Секретный источник",
	}
	cuts := newFakeDocumentCutStore(cut)
	cuts.pagesByCutID = map[int64][]*models.Page{7: {cutPage(10, 4, "Текст закрытой работы")}}
	doc := &models.Document{ID: 3, Slug: "r", MarkdownContent: `<cut id="7">`, PublishedAt: ptrTime(time.Now()),
		PublishedMarkdown: `<cut id="7">`}
	src := NewSEODocumentSource(&fakeDocumentStore{doc: doc}, cuts, &fakeWorkStore{}, markdown.NewRenderer()).
		WithClosed(closedWorks41())
	page, err := src.Page(context.Background(), "", "r")
	if err != nil || !strings.Contains(page.BodyHTML, "document-cut--unavailable") || strings.Contains(page.BodyHTML, "Секретный") {
		t.Fatalf("err=%v body=%s", err, page.BodyHTML)
	}
}

func TestDocumentCutFetchClosedIs404(t *testing.T) {
	cut, pages := longCutFixture(7)
	cut.WorkID = ptrInt64(41)
	newHandler := func() *DocumentCutHandler {
		return NewDocumentCutHandler(
			&fakeDocumentStore{doc: &models.Document{ID: 7, Slug: "razbor-7", MarkdownContent: `<cut id="9">`,
				PublishedAt: ptrTime(time.Now()), PublishedMarkdown: `<cut id="9">`}},
			&fakeCutFullStore{cut: cut, pages: pages},
			&fakeWorkStore{}, &fakePageStore{}, markdown.NewRenderer(),
		).WithClosed(closedWorks41())
	}

	rec := httptest.NewRecorder()
	newHandler().Full(rec, newCutFullRequest("razbor-7", "9", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404; тело: %s", rec.Code, rec.Body.String())
	}

	// Вклейка, которую тело не называет (черновик), отвечает 410 одинаково —
	// закрыта ли её работа, снаружи не видно.
	unnamed := NewDocumentCutHandler(
		&fakeDocumentStore{doc: &models.Document{ID: 7, Slug: "razbor-7", MarkdownContent: `<cut id="9">`,
			PublishedAt: ptrTime(time.Now()), PublishedMarkdown: "без вклеек"}},
		&fakeCutFullStore{cut: cut, pages: pages},
		&fakeWorkStore{}, &fakePageStore{}, markdown.NewRenderer(),
	).WithClosed(closedWorks41())
	rec = httptest.NewRecorder()
	unnamed.Full(rec, newCutFullRequest("razbor-7", "9", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("неназванная вклейка закрытой работы: код %d, ожидался 410", rec.Code)
	}

	rec = httptest.NewRecorder()
	newHandler().Full(rec, newCutFullRequest("razbor-7", "9", editorClaims()))
	if rec.Code != http.StatusOK {
		t.Fatalf("сотрудник: код %d, ожидался 200", rec.Code)
	}
}

func TestCreateCutOfClosedWorkIs404ForReader(t *testing.T) {
	pages := newFakePageStoreWithPages(&models.Page{ID: 10, WorkID: 41, PageNumber: 4, ContentMarkdown: "текст полосы"})
	owner := auth.Claims{UserID: 501, Role: models.RoleReader, Nickname: "хранитель"}
	run := func(claims *auth.Claims) (*httptest.ResponseRecorder, *fakeDocumentCutStore) {
		cuts := newFakeDocumentCutStore()
		h := (&DocumentCutHandler{
			documents: &fakeDocumentStore{doc: &models.Document{ID: 5, Slug: "razbor-5",
				AuthorNickname: "хранитель", OwnerID: ptrInt64(501)}},
			pages: pages, cuts: cuts, renderer: markdown.NewRenderer(),
		}).WithClosed(closedWorks41())
		body, _ := json.Marshal(map[string]any{"work_id": 41, "start_page": 4, "start_offset": 0,
			"end_page": 4, "end_offset": 5, "source_title": "Закрытая глава"})
		req := httptest.NewRequest(http.MethodPost, "/api/documents/хранитель/razbor-5/cuts", bytes.NewReader(body))
		req = mux.SetURLVars(req, map[string]string{"nickname": "хранитель", "slug": "razbor-5"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.Create)(rec, req)
		return rec, cuts
	}

	rec, cuts := run(&owner)
	if rec.Code != http.StatusNotFound || len(cuts.created) != 0 {
		t.Fatalf("читатель: код %d, создано %d; ожидался 404 без записи; %s", rec.Code, len(cuts.created), rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Закрытая глава") {
		t.Fatalf("подпись в ответе: %s", rec.Body.String())
	}
}

func TestStaticCollectionsSkipClosedItems(t *testing.T) {
	cols := staticCollectionsFake{
		list: []*models.Collection{{ID: 1, Slug: "s", Title: "Подборка", PublishedAt: ptrTime(time.Now())}},
		rows: []repository.ItemRow{closedItemRow(), chapterRow(12, 2, 101, 42)},
	}
	s := newStaticSourceForTest(nil, nil, nil, cols, nil).WithClosed(closedWorks41())
	got, err := s.Collections(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("err=%v got=%v", err, got)
	}
	raw, _ := json.Marshal(got)
	if len(got[0].Items) != 1 || strings.Contains(string(raw), closedTitle) {
		t.Fatalf("закрытый элемент попал в архив: %s", raw)
	}
}

// Кэш диапазона включён: прогретый сотрудником файл не-сотруднику не уходит.
func TestCollectionItemPagesClosedBeatsWarmCache(t *testing.T) {
	pages := &fakePageStore{getPageRangeFn: func(context.Context, int64, int, int) ([]*models.Page, error) {
		return []*models.Page{cutPage(1, 265, "текст закрытого")}, nil
	}}
	h := NewCollectionHandler(closedCollectionStore(), pages, markdown.NewRenderer(),
		NewRangeCache(pagecache.New(t.TempDir())), "", false).WithClosed(closedWorks41())

	staffRec := httptest.NewRecorder()
	withClaims(editorClaims(), h.ItemPages)(staffRec, itemPagesRequestFor("materializm", "11"))
	if staffRec.Code != http.StatusOK || !strings.Contains(staffRec.Body.String(), "текст закрытого") {
		t.Fatalf("сотрудник: %d %s", staffRec.Code, staffRec.Body.String())
	}
	rec := httptest.NewRecorder()
	h.ItemPages(rec, itemPagesRequestFor("materializm", "11"))
	if rec.Code != http.StatusGone || strings.Contains(rec.Body.String(), "текст закрытого") {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDownloadCollectionHandlerClosedItemUnnamed(t *testing.T) {
	// Работа 1 — тело главы «Глава» подборки-фикстуры.
	h := NewDownloadHandler(downloadCollectionFixture()).WithClosed(&fakeClosedStore{works: map[int64]string{1: models.ClosedByWork}})
	get := func(claims *auth.Claims) string {
		req := httptest.NewRequest(http.MethodGet, "/api/collections/moi/download?format=html", nil)
		req = mux.SetURLVars(req, map[string]string{"slug": "moi"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.Collection)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	if body := get(nil); !strings.Contains(body, "недоступный источник") {
		t.Fatalf("титул не говорит о недоступном источнике: %s", body)
	}
	if body := get(editorClaims()); strings.Contains(body, "недоступный источник") {
		t.Fatalf("сотруднику пункт назван недоступным: %s", body)
	}
}

func TestDocumentViewClosedCutUnavailableForReader(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(41), DocumentID: 3,
		Anchor: models.Anchor{StartPageID: 10, EndPageID: 10, EndOffset: 5},
		Status: models.CutStatusOK, SourceTitle: "Секретный источник",
	}
	cuts := newFakeDocumentCutStore(cut)
	cuts.pagesByCutID = map[int64][]*models.Page{7: {cutPage(10, 4, "Текст закрытой работы")}}
	works := &fakeWorkStore{getFn: func(context.Context, int64) (*models.Work, error) { return &models.Work{ID: 41}, nil }}
	h := (&DocumentHandler{
		documentRepo: &fakeDocumentStore{doc: &models.Document{ID: 3, Slug: "r", MarkdownContent: `<cut id="7">`,
			PublishedMarkdown: `<cut id="7">`, PublishedAt: ptrTime(time.Now())}},
		renderer: markdown.NewRenderer(), cuts: cuts, works: works,
	}).WithClosed(closedWorks41())

	view := func(claims *auth.Claims) string {
		req := httptest.NewRequest(http.MethodGet, "/api/documents/r/view", nil)
		req = mux.SetURLVars(req, map[string]string{"slug": "r"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.View)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	if body := view(nil); !strings.Contains(body, "document-cut--unavailable") || strings.Contains(body, "Секретный") {
		t.Fatalf("читателю: %s", body)
	}
	if body := view(editorClaims()); strings.Contains(body, "document-cut--unavailable") {
		t.Fatalf("сотруднику: %s", body)
	}
}
