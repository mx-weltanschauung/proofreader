package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// chapterSet — подделка хранилища закрытых глав: id → закрыта, плюс диапазоны
// полос по работам (ClosedSpansStore).
type chapterSet struct {
	chapters map[int64]bool
	spans    map[int64][]models.PageSpan
}

func (c chapterSet) AllClosedChapterIDs(context.Context) (map[int64]bool, error) {
	return c.chapters, nil
}
func (c chapterSet) ClosedSpans(_ context.Context, w int64) ([]models.PageSpan, error) {
	return c.spans[w], nil
}
func (c chapterSet) ClosedChapterIDs(context.Context, int64) (map[int64]bool, error) {
	return c.chapters, nil
}

func chapterTgt(workID, chapterID int64) models.EventTarget {
	w, c := workID, chapterID
	return models.EventTarget{Kind: models.EventTargetChapter, WorkID: &w, ChapterID: &c, Title: "Глава"}
}

// ---- события ----

func TestEventTargetClosedChapterHidden(t *testing.T) {
	d := &models.EventDetail{
		Event:        models.Event{Slug: "i-sezd", Title: "I съезд"},
		Organization: models.EventOrganization{Slug: "org-a"},
		Sections: []models.EventSection{
			{Position: 0, Title: "Открытый пункт", Entries: []models.EventEntry{
				{Label: "Смешанная строка", Targets: []models.EventTarget{chapterTgt(5, 70), chapterTgt(5, 71)}},
				{Label: "Секретная подпись", Targets: []models.EventTarget{chapterTgt(5, 70)}},
			}},
			{Position: 1, Title: "Секретный пункт", Entries: []models.EventEntry{
				{Label: "Другая секретная", Targets: []models.EventTarget{chapterTgt(5, 70)}},
			}},
		},
	}
	got, ok := visibleEvent(d, nil, ClosedSet{70: true}, false)
	if !ok || len(got.Sections) != 1 || len(got.Sections[0].Entries) != 1 || len(got.Sections[0].Entries[0].Targets) != 1 {
		t.Fatalf("осталось не то: ok=%v %+v", ok, got.Sections)
	}
	raw, _ := json.Marshal(got)
	for _, leak := range []string{"Секретная подпись", "Секретный пункт", "Другая секретная"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("утечка %q: %s", leak, raw)
		}
	}
	staff, _ := visibleEvent(d, nil, ClosedSet{70: true}, true)
	if len(staff.Sections) != 2 || !staff.Sections[0].Entries[0].Targets[0].Closed {
		t.Errorf("сотруднику закрытая цель без пометки: %+v", staff.Sections)
	}
	if staff.Sections[0].Entries[0].Targets[1].Closed {
		t.Error("открытая цель помечена закрытой")
	}
}

func TestVisibleOrganizationCountsClosedChapterTargets(t *testing.T) {
	org := &models.EventOrganizationDetail{Kinds: []models.EventKindEvents{{Events: []models.EventSummary{
		{Slug: "a", TargetWorkIDs: []int64{5, 5}, TargetChapterIDs: []int64{70, 71}},
		{Slug: "b", TargetWorkIDs: []int64{5}, TargetChapterIDs: []int64{70}},
	}}}}
	got, ok := visibleOrganization(org, nil, ClosedSet{70: true}, false)
	if !ok || len(got.Kinds[0].Events) != 1 || got.Kinds[0].Events[0].Slug != "a" || got.Kinds[0].Events[0].MaterialsTotal != 1 {
		t.Fatalf("ok=%v %+v", ok, got.Kinds)
	}
}

func TestChapterEventsClosed404(t *testing.T) {
	store := &fakeEventStore{links: []models.ChapterEventLink{{EventSlug: "i-sezd"}}}
	h := NewEventHandler(store, nil).WithClosedChapters(chapterSet{chapters: map[int64]bool{70: true}})
	call := func(claims *auth.Claims, chapter string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req = mux.SetURLVars(req, map[string]string{"workId": "5", "id": chapter})
		rec := httptest.NewRecorder()
		withClaims(claims, h.ChapterEvents)(rec, req)
		return rec.Code
	}
	if c := call(nil, "70"); c != http.StatusNotFound {
		t.Errorf("аноним, закрытая: %d", c)
	}
	if c := call(readerClaims(5, "чтец"), "70"); c != http.StatusNotFound {
		t.Errorf("читатель, закрытая: %d", c)
	}
	if c := call(editorClaims(), "70"); c != http.StatusOK {
		t.Errorf("сотрудник, закрытая: %d", c)
	}
	if c := call(nil, "71"); c != http.StatusOK {
		t.Errorf("аноним, открытая: %d", c)
	}
}

// ---- озвучка ----

func audioClosedFixture() (*AudioHandler, *fakeAudio) {
	f := &fakeAudio{
		tracks: []models.AudioTrack{
			{ID: 1, WorkID: 8, Title: "Открытая", StartPage: 1, EndPage: 2, S3Key: "works/8/r/1-2-p-t.opus"},
			{ID: 2, WorkID: 8, Title: "Через закрытую", StartPage: 3, EndPage: 6, S3Key: "works/8/r/3-6-p-t.opus"},
		},
		recs: []models.AudioRecording{
			{ID: 7, WorkID: 8, ChapterID: 70, ChapterTitle: "Закрытая", S3Key: "works/8/rec/70/x.mp3", ChapterStartPage: 5, ChapterEndPage: 5},
			{ID: 8, WorkID: 8, ChapterID: 71, ChapterTitle: "Открытая", S3Key: "works/8/rec/71/x.mp3", ChapterStartPage: 1, ChapterEndPage: 2},
			// Запись открытой родительской главы, внутри которой закрытая полоса 5.
			{ID: 9, WorkID: 8, ChapterID: 72, ChapterTitle: "Родитель", S3Key: "works/8/rec/72/x.mp3", ChapterStartPage: 3, ChapterEndPage: 8},
		},
	}
	cs := chapterSet{chapters: map[int64]bool{70: true}, spans: map[int64][]models.PageSpan{8: {{Start: 5, End: 5}}}}
	h := NewAudioHandler(f, f, f, fakeAudioChapters{}, storage.NewMemoryStorage(), nil).WithClosedSpans(cs)
	return h, f
}

func TestAudioTrackOverClosedPageHidden(t *testing.T) {
	h, _ := audioClosedFixture()
	get := func(claims *auth.Claims) (tracks, recs int) {
		req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"id": "8"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.WorkAudio)(rec, req)
		var out struct {
			Tracks     []struct{ ID int64 } `json:"tracks"`
			Recordings []struct{ ID int64 } `json:"recordings"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err, rec.Body)
		}
		return len(out.Tracks), len(out.Recordings)
	}
	for name, claims := range map[string]*auth.Claims{"аноним": nil, "читатель": readerClaims(5, "чтец")} {
		if tr, rc := get(claims); tr != 1 || rc != 1 { // запись родителя (id 9) тоже скрыта
			t.Errorf("%s: дорожек %d, записей %d, ожидалось по 1", name, tr, rc)
		}
	}
	if tr, rc := get(editorClaims()); tr != 2 || rc != 3 {
		t.Errorf("сотрудник: дорожек %d, записей %d, ожидалось 2 и 3", tr, rc)
	}

	redirect := func(fn http.HandlerFunc, id string, claims *auth.Claims) int {
		req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"id": id})
		rec := httptest.NewRecorder()
		withClaims(claims, fn)(rec, req)
		return rec.Code
	}
	if c := redirect(h.TrackRedirect, "2", nil); c != http.StatusNotFound {
		t.Errorf("дорожка через закрытую полосу: %d", c)
	}
	if c := redirect(h.TrackRedirect, "1", nil); c != http.StatusFound {
		t.Errorf("открытая дорожка: %d", c)
	}
	if c := redirect(h.TrackRedirect, "2", editorClaims()); c != http.StatusFound {
		t.Errorf("сотрудник, дорожка: %d", c)
	}
	if c := redirect(h.RecordingRedirect, "7", nil); c != http.StatusNotFound {
		t.Errorf("запись в закрытой главе: %d", c)
	}
	if c := redirect(h.RecordingRedirect, "9", nil); c != http.StatusNotFound {
		t.Errorf("запись открытого родителя над закрытой полосой: %d", c)
	}
	if c := redirect(h.RecordingRedirect, "9", editorClaims()); c != http.StatusFound {
		t.Errorf("сотрудник, запись родителя: %d", c)
	}
	if c := redirect(h.RecordingRedirect, "8", nil); c != http.StatusFound {
		t.Errorf("открытая запись: %d", c)
	}
	if c := redirect(h.RecordingRedirect, "7", editorClaims()); c != http.StatusFound {
		t.Errorf("сотрудник, запись: %d", c)
	}
}

func TestAudioPlaylistSkipsClosed(t *testing.T) {
	_, f := audioClosedFixture()
	cs := chapterSet{chapters: map[int64]bool{70: true}, spans: map[int64][]models.PageSpan{47: {{Start: 15, End: 15}}}}
	f.tracks = []models.AudioTrack{
		{ID: 2, WorkID: 47, Title: "Открытая", StartPage: 10, EndPage: 12},
		{ID: 3, WorkID: 47, Title: "Через закрытую", StartPage: 13, EndPage: 16},
	}
	f.recs = []models.AudioRecording{{ID: 7, WorkID: 47, ChapterID: 70, ChapterTitle: "Закрытая",
		ChapterStartPage: 15, ChapterEndPage: 15, Position: 1},
		{ID: 8, WorkID: 47, ChapterID: 72, ChapterTitle: "Родитель",
			ChapterStartPage: 10, ChapterEndPage: 20, Position: 1}}
	h := NewDownloadHandler(nil).WithAudio(
		NewAudioPlaylist(f, f, fakePlaylistWorks{}, fakePlaylistChapters{}, "https://lib.example").WithClosedSpans(cs))
	get := func(claims *auth.Claims) string {
		req := mux.SetURLVars(httptest.NewRequest("GET", "/x?format=m3u", nil), map[string]string{"id": "47"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.Work)(rec, req)
		return rec.Body.String()
	}
	if b := get(nil); !strings.Contains(b, "/api/audio/2.opus") || strings.Contains(b, "/api/audio/3.opus") || strings.Contains(b, "/rec/7") || strings.Contains(b, "/rec/8") {
		t.Errorf("аноним:\n%s", b)
	}
	if b := get(editorClaims()); !strings.Contains(b, "/api/audio/3.opus") || !strings.Contains(b, "/rec/7") || !strings.Contains(b, "/rec/8") {
		t.Errorf("сотрудник:\n%s", b)
	}
}

// ---- подборки ----

func TestCollectionItemClosedChapterUnavailable(t *testing.T) {
	store := collectionFixture()
	store.chapters = []*models.Chapter{{ID: 100, WorkID: 41, Title: closedTitle, StartPage: 265, EndPage: 313,
		Closed: true, ClosedBy: models.ChapterClosedBySelf}}
	store.rows = []repository.ItemRow{closedItemRow()}
	store.item = &models.CollectionItem{ID: 11, CollectionID: 1, Kind: models.CollectionItemKindChapter,
		ChapterID: ptrInt64(100), WorkID: ptrInt64(41), SnapshotTitle: closedTitle}
	h := closedCollectionHandler(store, &fakeClosedStore{})

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil), map[string]string{"slug": "materializm"})
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, closedTitle) || strings.Contains(body, "Людвиг") || strings.Contains(body, "Том 21") ||
		strings.Contains(body, "closed") || !strings.Contains(body, `"unavailable":true`) {
		t.Fatalf("читателю: %s", body)
	}
	rec = httptest.NewRecorder()
	withClaims(editorClaims(), h.Get)(rec, req)
	if !strings.Contains(rec.Body.String(), closedTitle) || strings.Contains(rec.Body.String(), `"unavailable"`) {
		t.Fatalf("сотруднику: %s", rec.Body.String())
	}

	// Страницы пункта — как у пункта на закрытой работе: 410.
	rec = httptest.NewRecorder()
	h.ItemPages(rec, itemPagesRequestFor("materializm", "11"))
	if rec.Code != http.StatusGone {
		t.Fatalf("страницы пункта закрытой главы: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	withClaims(editorClaims(), h.ItemPages)(rec, itemPagesRequestFor("materializm", "11"))
	if rec.Code != http.StatusOK {
		t.Fatalf("сотрудник: %d", rec.Code)
	}
}

func TestCollectionSubtreeSkipsClosedChild(t *testing.T) {
	chapters := []*models.Chapter{
		{ID: 100, WorkID: 41, Title: "Корень", StartPage: 1, EndPage: 30},
		{ID: 101, WorkID: 41, ParentID: ptrInt64(100), Title: "Открытая подглава", StartPage: 1, EndPage: 10},
		{ID: 102, WorkID: 41, ParentID: ptrInt64(100), Title: "Тайная подглава", StartPage: 11, EndPage: 20,
			Closed: true, ClosedBy: models.ChapterClosedBySelf},
		{ID: 103, WorkID: 41, ParentID: ptrInt64(102), Title: "Потомок тайной", StartPage: 11, EndPage: 15,
			ClosedBy: models.ChapterClosedByParent},
	}
	rows := []repository.ItemRow{chapterRow(1, 1, 100, 41)}
	reader := buildCollectionTOC(rows, chapters, nil, true)
	raw, _ := json.Marshal(reader)
	if strings.Contains(string(raw), "Тайная") || strings.Contains(string(raw), "Потомок") || !strings.Contains(string(raw), "Открытая подглава") {
		t.Fatalf("читателю: %s", raw)
	}
	staff := buildCollectionTOC(rows, chapters, nil, false)
	raw, _ = json.Marshal(staff)
	if !strings.Contains(string(raw), "Тайная подглава") || !strings.Contains(string(raw), "Потомок тайной") {
		t.Fatalf("сотруднику: %s", raw)
	}
}

// ---- вклейки ----

func TestCutFromClosedPageUnavailable(t *testing.T) {
	cut := &models.DocumentCut{
		ID: 7, WorkID: ptrInt64(1), DocumentID: 3,
		Anchor: models.Anchor{StartPageID: 10, EndPageID: 11, EndOffset: 5},
		Status: models.CutStatusOK, SourceTitle: "Секретный источник",
	}
	cuts := newFakeDocumentCutStore(cut)
	cuts.pagesByCutID = map[int64][]*models.Page{7: {cutPage(10, 4, "Открытая"), cutPage(11, 5, "Текст закрытой главы")}}
	doc := &models.Document{ID: 3, MarkdownContent: `<cut id="7">`}
	works := &fakeWorkStore{getFn: func(context.Context, int64) (*models.Work, error) { return &models.Work{ID: 1}, nil }}

	html, _, err := assembleDocumentFor(context.Background(), markdown.NewRenderer(), cuts, works, doc, nil,
		spanStore{1: {{Start: 5, End: 5}}})
	if err != nil || !strings.Contains(html, "document-cut--unavailable") ||
		strings.Contains(html, "Секретный источник") || strings.Contains(html, "закрытой главы") {
		t.Fatalf("err=%v html=%s", err, html)
	}
	// Без закрытых полос в диапазоне — собирается как прежде.
	html, _, _ = assembleDocumentFor(context.Background(), markdown.NewRenderer(), cuts, works, doc, nil,
		spanStore{1: {{Start: 9, End: 9}}})
	if strings.Contains(html, "document-cut--unavailable") || !strings.Contains(html, "document-cut-body") {
		t.Fatalf("открытая вклейка: %s", html)
	}

	// Догрузка: 404 не-сотруднику, сотруднику — вклейка.
	full := func(claims *auth.Claims) int {
		h := NewDocumentCutHandler(
			&fakeDocumentStore{doc: &models.Document{ID: 7, Slug: "razbor-7", MarkdownContent: `<cut id="9">`,
				PublishedAt: ptrTime(time.Now()), PublishedMarkdown: `<cut id="9">`}},
			&fakeCutFullStore{cut: func() *models.DocumentCut { c, _ := longCutFixture(7); return c }(),
				pages: func() []*models.Page { _, p := longCutFixture(7); return p }()},
			&fakeWorkStore{}, &fakePageStore{}, markdown.NewRenderer(),
		).WithClosedSpans(spanStore{1: {{Start: 5, End: 5}}})
		rec := httptest.NewRecorder()
		h.Full(rec, newCutFullRequest("razbor-7", "9", claims))
		return rec.Code
	}
	if c := full(nil); c != http.StatusNotFound {
		t.Errorf("аноним: %d", c)
	}
	if c := full(editorClaims()); c != http.StatusOK {
		t.Errorf("сотрудник: %d", c)
	}
}

// ---- выгрузка ----

func TestDownloadETagChangesOnChapterClose(t *testing.T) {
	etag := func(spans spanStore, wrap bool, claims *auth.Claims) (string, string) {
		src := downloadFixture()
		if wrap {
			src.pages = NewViewPages(src.pages, spans)
		}
		h := NewDownloadHandler(src)
		req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x?format=html", nil), map[string]string{"id": "1"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.Work)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body)
		}
		ctx := context.Background()
		if claims != nil {
			ctx = context.WithValue(ctx, middleware.UserContextKey, claims)
		}
		b, _ := src.Work(ctx, 1)
		variant := ""
		if b != nil {
			variant = b.Meta.CacheVariant
		}
		return rec.Header().Get("ETag"), variant
	}
	base, _ := etag(nil, false, nil)
	emptyView, v0 := etag(spanStore{}, true, nil)
	if emptyView != base || v0 != "" {
		t.Errorf("том без закрытых полос сменил ETag: %s -> %s (%q)", base, emptyView, v0)
	}
	closed, v1 := etag(spanStore{1: {{Start: 3, End: 3}}}, true, nil)
	if closed == base || !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(v1) {
		t.Errorf("закрытие главы не сменило ETag: %s -> %s (%q)", base, closed, v1)
	}
	other, v2 := etag(spanStore{1: {{Start: 3, End: 4}}}, true, nil)
	if other == closed || v2 == v1 {
		t.Errorf("другой диапазон дал тот же ETag/отпечаток")
	}
	staff, vs := etag(spanStore{1: {{Start: 3, End: 3}}}, true, editorClaims())
	if staff != base || vs != "" {
		t.Errorf("сотрудник получил отпечаток: %s %q", staff, vs)
	}
}

func TestCollectionAddItemClosedChapterIs404ForReader(t *testing.T) {
	owner := testCollectionOwnerClaims.UserID
	newStore := func(owned bool) *fakeCollectionStore {
		s := collectionFixture()
		if owned {
			s.collections[0].OwnerID = &owner
			s.collections[0].AuthorNickname = testCollectionOwnerClaims.Nickname
		}
		s.chapterWork = map[int64]int64{100: 41, 101: 41}
		s.chapters = []*models.Chapter{
			{ID: 100, WorkID: 41, Title: "Закрытая", ClosedBy: models.ChapterClosedBySelf, Closed: true},
			{ID: 101, WorkID: 41, Title: "Открытая"},
		}
		return s
	}
	add := func(store *fakeCollectionStore, claims *auth.Claims, chapter string) int {
		h := closedCollectionHandler(store, &fakeClosedStore{})
		body := `{"kind":"chapter","chapter_id":` + chapter + `}`
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		vars := map[string]string{"slug": "materializm"}
		if claims == testCollectionOwnerClaims {
			vars = map[string]string{"nickname": owner2nick(), "slug": "materializm"}
		}
		rec := httptest.NewRecorder()
		withClaims(claims, h.AddItem)(rec, mux.SetURLVars(req, vars))
		return rec.Code
	}
	store := newStore(true)
	if c := add(store, testCollectionOwnerClaims, "100"); c != http.StatusNotFound || store.addedKind != "" {
		t.Errorf("читатель, закрытая глава: %d, добавлено %q", c, store.addedKind)
	}
	if c := add(newStore(true), testCollectionOwnerClaims, "101"); c != http.StatusCreated {
		t.Errorf("читатель, открытая глава: %d", c)
	}
	staffStore := newStore(false)
	staffStore.collections[0].OwnerID = nil
	if c := add(staffStore, editorClaims(), "100"); c != http.StatusCreated {
		t.Errorf("сотрудник, закрытая глава: %d", c)
	}
}

func TestDownloadCollectionClosedChapterItemUnnamed(t *testing.T) {
	src := downloadCollectionFixture()
	src.collections.(*fakeCollectionStoreForDownload).chapters = []*models.Chapter{
		{ID: 10, WorkID: 1, Title: "Глава", StartPage: 2, EndPage: 4, Closed: true, ClosedBy: models.ChapterClosedBySelf},
	}
	h := NewDownloadHandler(src)
	get := func(claims *auth.Claims) string {
		req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x?format=html", nil), map[string]string{"slug": "moi"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.Collection)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	if body := get(nil); !strings.Contains(body, "недоступный источник") || strings.Contains(body, "Начало главы") {
		t.Fatalf("читателю: %s", body)
	}
	if body := get(editorClaims()); strings.Contains(body, "недоступный источник") || !strings.Contains(body, "Начало главы") {
		t.Fatalf("сотруднику: %s", body)
	}
}

func TestDownloadCollectionHidesClosedSubchapterHeading(t *testing.T) {
	src := downloadCollectionFixture()
	src.collections.(*fakeCollectionStoreForDownload).chapters = []*models.Chapter{
		{ID: 10, WorkID: 1, Title: "Глава", StartPage: 2, EndPage: 4},
		{ID: 11, WorkID: 1, ParentID: ptrInt64(10), Title: "СЕКРЕТНАЯПОДГЛАВА", StartPage: 3, EndPage: 3,
			Closed: true, ClosedBy: models.ChapterClosedBySelf},
	}
	src.pages = NewViewPages(src.pages, spanStore{1: {{Start: 3, End: 3}}})
	h := NewDownloadHandler(src)
	get := func(claims *auth.Claims) string {
		req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x?format=html", nil), map[string]string{"slug": "moi"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.Collection)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код %d: %s", rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	if body := get(nil); strings.Contains(body, "СЕКРЕТНАЯПОДГЛАВА") || !strings.Contains(body, "Начало главы") {
		t.Fatalf("аноним: %s", body)
	}
	if body := get(editorClaims()); !strings.Contains(body, "СЕКРЕТНАЯПОДГЛАВА") {
		t.Fatalf("сотрудник: %s", body)
	}
}

// ---- избранное собрания и «мои правки» (сквозной сторож, задача 10) ----

// TestHighlightsHideClosedChapter — пункт избранного на закрытой главе
// открытой работы несёт её заглавие и подпись: не-сотрудник его не видит,
// сотрудник видит, сбой проверки — 500, а не полный список.
func TestHighlightsHideClosedChapter(t *testing.T) {
	store := &fakeHighlightStore{edition: &models.Edition{ID: 4}, list: []models.EditionHighlight{
		{ChapterID: 7, WorkID: 1, ChapterTitle: "Речь закрытая", Label: "закрытый пункт"},
		{ChapterID: 8, WorkID: 1, ChapterTitle: "Речь открытая", Label: "открытый пункт"},
	}}
	get := func(claims *auth.Claims, chapters closedChapterLister) *httptest.ResponseRecorder {
		h := NewHighlightHandler(store).WithClosedChapters(chapters)
		req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/editions/4/highlights", nil), map[string]string{"id": "4"})
		rec := httptest.NewRecorder()
		withClaims(claims, h.Get)(rec, req)
		return rec
	}
	closed := chapterSet{chapters: map[int64]bool{7: true}}
	rec := get(nil, closed)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "закрыт") ||
		!strings.Contains(rec.Body.String(), "открытый пункт") {
		t.Errorf("аноним: %d %s — ждали только открытый пункт", rec.Code, rec.Body.String())
	}
	if rec = get(testReaderClaims, closed); strings.Contains(rec.Body.String(), "закрыт") {
		t.Errorf("читатель: %s — закрытый пункт виден", rec.Body.String())
	}
	if rec = get(editorClaims(), closed); !strings.Contains(rec.Body.String(), "закрытый пункт") {
		t.Errorf("сотрудник: %s — закрытый пункт обязан видеть", rec.Body.String())
	}
	if rec = get(nil, failingChapterLister{}); rec.Code != http.StatusInternalServerError {
		t.Errorf("сбой проверки закрытых глав: %d, ждали 500", rec.Code)
	}
}

type failingChapterLister struct{}

func (failingChapterLister) AllClosedChapterIDs(context.Context) (map[int64]bool, error) {
	return nil, errors.New("база недоступна")
}

// TestMineHidesSuggestionsOnClosedChapterPages — правка на полосе закрытой
// главы открытой работы называет номер полосы и заглавие тома, то есть место
// закрытой речи: читателю «моё» её не называет, правку на открытой полосе той
// же работы — называет; сотруднику — обе.
func TestMineHidesSuggestionsOnClosedChapterPages(t *testing.T) {
	rows := []repository.SuggestionRow{
		{ID: 1, WorkID: 42, PageNumber: 3, Note: "на закрытой"},
		{ID: 2, WorkID: 42, PageNumber: 1, Note: "на открытой"},
	}
	spans := chapterSet{spans: map[int64][]models.PageSpan{42: {{Start: 2, End: 3}}}}
	mine := func(claims *auth.Claims) *httptest.ResponseRecorder {
		store := &fakeSuggestionStore{recentIPs: map[string]int{}, rows: append([]repository.SuggestionRow{}, rows...)}
		h := NewPageSuggestionHandler(store, NewViewPages(&fakePageStore{}, spans), &fakePageVersions{},
			&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
		rec := httptest.NewRecorder()
		withClaims(claims, h.Mine)(rec, httptest.NewRequest(http.MethodGet, "/api/suggestions/mine", nil))
		return rec
	}
	rec := mine(testReaderClaims)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "на закрытой") ||
		!strings.Contains(rec.Body.String(), "на открытой") {
		t.Errorf("читатель: %d %s — ждали только правку на открытой полосе", rec.Code, rec.Body.String())
	}
	if rec = mine(editorClaims()); !strings.Contains(rec.Body.String(), "на закрытой") {
		t.Errorf("сотрудник: %s — правку на закрытой полосе обязан видеть", rec.Body.String())
	}
}
