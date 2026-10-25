package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/pagecache"
	"proofreader/internal/repository"
	"proofreader/pkg/markdown"
	"proofreader/pkg/storage"
)

// Том 1, полосы 1..20, текст полосы — «Полоса N.». Главы:
//
//	1  «Открытая»  1..3
//	3  «Закрытая»  4..6 (своим признаком), подглава 31 5..5 (через родителя)
//	2  «Соседка»   6..9 — полоса 6 общая с закрытой
//	4  «Родитель» 10..20, подглавы 41 «Речь» 11..12 (закрыта) и 42 «Прения» 13..14
//
// Том 2 — полосы 1..5, ничего не закрыто. У обоих томов есть оригинал.
func closedChapterRows() map[int64]*models.Chapter {
	return map[int64]*models.Chapter{
		1:  {ID: 1, WorkID: 1, Title: "Открытая", Type: "chapter", StartPage: 1, EndPage: 3, OrderNumber: 1},
		3:  {ID: 3, WorkID: 1, Title: "Закрытая", Type: "chapter", StartPage: 4, EndPage: 6, OrderNumber: 2, Closed: true, ClosedBy: "chapter"},
		31: {ID: 31, WorkID: 1, Title: "Подглава закрытой", Type: "chapter", StartPage: 5, EndPage: 5, ParentID: i64(3), ClosedBy: "parent"},
		2:  {ID: 2, WorkID: 1, Title: "Соседка", Type: "chapter", StartPage: 6, EndPage: 9, OrderNumber: 3},
		4:  {ID: 4, WorkID: 1, Title: "Родитель", Type: "chapter", StartPage: 10, EndPage: 20, OrderNumber: 4},
		41: {ID: 41, WorkID: 1, Title: "Речь", Type: "chapter", StartPage: 11, EndPage: 12, ParentID: i64(4), Closed: true, ClosedBy: "chapter"},
		42: {ID: 42, WorkID: 1, Title: "Прения", Type: "chapter", StartPage: 13, EndPage: 14, ParentID: i64(4)},
	}
}

// chapterClosure — закрытость тома 1: диапазоны и закрытые главы.
type chapterClosure struct{ spanStore }

func (chapterClosure) ClosedChapterIDs(_ context.Context, workID int64) (map[int64]bool, error) {
	if workID != 1 {
		return map[int64]bool{}, nil
	}
	return map[int64]bool{3: true, 31: true, 41: true}, nil
}

func closedChapterSpans() chapterClosure {
	return chapterClosure{spanStore{1: {{Start: 4, End: 6}, {Start: 5, End: 5}, {Start: 11, End: 12}}}}
}

// closedChapterStore — голое хранилище глав тома 1, как репозиторий: закрытое
// отдаёт всем, с Closed/ClosedBy в строке. deletes считает удаления.
func closedChapterStore(deletes *int) *fakeChapterReader {
	return &fakeChapterReader{fakeChapterStore: &fakeChapterStore{
		getByIDFn: func(_ context.Context, id int64) (*models.Chapter, error) {
			if c, ok := closedChapterRows()[id]; ok {
				return c, nil
			}
			return nil, errHiddenChapter
		},
		listHierarchicalFn: func(_ context.Context, w int64) ([]*models.Chapter, error) {
			if w != 1 {
				return []*models.Chapter{}, nil
			}
			m := closedChapterRows()
			m[3].Children = []*models.Chapter{m[31]}
			m[4].Children = []*models.Chapter{m[41], m[42]}
			return []*models.Chapter{m[1], m[3], m[2], m[4]}, nil
		},
		deleteFn: func(context.Context, int64) error { *deletes++; return nil },
	}}
}

func numberedPages(workID int64, n int) []*models.Page {
	ps := memPages(workID, n)
	for _, p := range ps {
		p.ContentMarkdown = fmt.Sprintf("Полоса %d.", p.PageNumber)
		p.PreviewPath = fmt.Sprintf("works/%d/pages/page_%d.png", workID, p.PageNumber)
	}
	return ps
}

// filedWorks — WorkRepo с оригиналом у каждой работы.
type filedWorks struct {
	*fakeWorkRepo
	works map[int64]*models.Work
}

func (f *filedWorks) GetByID(_ context.Context, id int64) (*models.Work, error) {
	if w, ok := f.works[id]; ok {
		cp := *w
		return &cp, nil
	}
	return nil, fmt.Errorf("work not found")
}

func (f *filedWorks) List(context.Context, int, int, *models.WorkStatus, *int64) ([]*models.Work, error) {
	return []*models.Work{f.works[1], f.works[2]}, nil
}

// closedChapterWorks — ClosedChapterWorks по готовому множеству.
type closedChapterWorks map[int64]bool

func (c closedChapterWorks) WorksWithClosedChapters(context.Context) (map[int64]bool, error) {
	return c, nil
}

type closedChaptersFixture struct {
	handler http.Handler
	cache   *pagecache.Store
	deletes *int
}

// newClosedChaptersFixture — настоящий роутер над обработчиками, собранными
// так же, как в NewServer: полосы через ViewPages, главы читателям через
// ViewChapters, обработчик глав — над голым хранилищем со своей обёрткой.
func newClosedChaptersFixture(t *testing.T) *closedChaptersFixture {
	t.Helper()
	deletes := 0
	rawChapters := closedChapterStore(&deletes)
	spans := closedChapterSpans()
	viewPages := NewViewPages(memPageStore(numberedPages(1, 20), numberedPages(2, 5)), spans)
	viewChapters := NewViewChapters(rawChapters, spans)
	renderer := markdown.NewRenderer()
	scans := storage.NewMemoryStorage()
	store := pagecache.New(t.TempDir())

	ph := NewPageHandler(viewPages, &fakePageVersions{}, &fakeFragmentStore{}, &fakeDocumentCutStore{}, renderer, scans, time.Minute)
	ch := NewChapterHandler(rawChapters, viewPages, renderer, NewRangeCache(store)).
		WithClosedChapters(spans)
	rh := NewReadingHandler(viewPages, renderer).WithClosedMarkers(viewChapters)
	ih := NewIndexHandler(nil, nil, nil, viewPages, viewChapters, nil, renderer)
	suggestions := &fakeSuggestionStore{recentIPs: map[string]int{}, detail: &repository.SuggestionDetail{
		SuggestionRow: repository.SuggestionRow{ID: 3, PageID: 1005}, CurrentMarkdown: "Полоса 5.",
	}}
	sh := NewPageSuggestionHandler(suggestions, viewPages, &fakePageVersions{},
		&fakeFragmentStore{}, &fakeDocumentCutStore{}, "соль", false)
	works := &filedWorks{fakeWorkRepo: &fakeWorkRepo{}, works: map[int64]*models.Work{
		1: {ID: 1, Title: "Том с закрытой главой", FilePath: "works/1/original/a.pdf"},
		2: {ID: 2, Title: "Том без закрытого", FilePath: "works/2/original/b.pdf"},
	}}
	wh := NewWorkHandler(works, viewPages, renderer, scans, time.Minute, nil, nil).
		WithClosedChapterWorks(closedChapterWorks{1: true})

	rt := createTestRouter()
	r := NewRouter(nil, wh, ph, ch, nil, nil, nil, nil, nil, nil, nil, nil, ih, nil, nil, rh, nil, nil, sh, nil, nil,
		rt.cacheHandler, nil, rt.authService).WithClosed(&fakeClosedStore{})
	return &closedChaptersFixture{handler: r.Setup(), cache: store, deletes: &deletes}
}

func (f *closedChaptersFixture) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *closedChaptersFixture) get(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, http.MethodGet, path, token, nil)
}

// hiddenTokens — кто закрытого не видит.
func hiddenTokens(t *testing.T) map[string]string {
	return map[string]string{"аноним": "", "читатель": readerToken(t)}
}

func staffTokens(t *testing.T) map[string]string {
	return map[string]string{"редактор": editorToken(t), "администратор": adminToken(t)}
}

func decodeChapterPages(t *testing.T, rec *httptest.ResponseRecorder) ChapterPagesResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ждали 200: %s", rec.Code, rec.Body)
	}
	var body ChapterPagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело: %v", err)
	}
	return body
}

func chapterPageNums(pages []ChapterPage) []int {
	out := make([]int, len(pages))
	for i, p := range pages {
		out[i] = p.PageNumber
	}
	return out
}

func TestClosedChapterGet404(t *testing.T) {
	f := newClosedChaptersFixture(t)
	for name, token := range hiddenTokens(t) {
		for _, path := range []string{"/api/works/1/chapters/3", "/api/works/1/chapters/31", "/api/works/1/chapters/41",
			"/api/works/1/chapters/3/pages", "/api/works/1/chapters/41/pages"} {
			if rec := f.get(t, path, token); rec.Code != http.StatusNotFound {
				t.Errorf("%s, %s: код %d, ждали 404", name, path, rec.Code)
			}
		}
		rec := f.get(t, "/api/works/1/chapters", token)
		var tree []*models.Chapter
		_ = json.Unmarshal(rec.Body.Bytes(), &tree)
		if got := treeIDs(tree); !slices.Equal(got, []int64{1, 2, 4, 42}) {
			t.Errorf("%s: оглавление %v, ждали [1 2 4 42]", name, got)
		}
		if rec := f.get(t, "/api/works/1/chapters/2", token); rec.Code != http.StatusOK {
			t.Errorf("%s: открытая соседка — код %d", name, rec.Code)
		}
	}
	for name, token := range staffTokens(t) {
		for _, path := range []string{"/api/works/1/chapters/3", "/api/works/1/chapters/31", "/api/works/1/chapters/41/pages"} {
			if rec := f.get(t, path, token); rec.Code != http.StatusOK {
				t.Errorf("%s, %s: код %d, ждали 200", name, path, rec.Code)
			}
		}
		rec := f.get(t, "/api/works/1/chapters", token)
		var tree []*models.Chapter
		_ = json.Unmarshal(rec.Body.Bytes(), &tree)
		if got := treeIDs(tree); !slices.Equal(got, []int64{1, 3, 31, 2, 4, 41, 42}) {
			t.Errorf("%s: оглавление %v", name, got)
		}
	}
}

func TestPageOfClosedChapter404(t *testing.T) {
	f := newClosedChaptersFixture(t)
	closedPaths := []string{
		"/api/works/1/pages/1005", "/api/works/1/pages/1005/blocks", "/api/works/1/pages/1005/render",
		"/api/works/1/pages/1005/concepts", "/api/works/1/pages/by-number/5",
		// общая с соседкой полоса закрыта целиком
		"/api/works/1/pages/1006", "/api/works/1/pages/by-number/6", "/api/works/1/pages/1006/render",
		"/api/works/1/pages/1011/blocks",
	}
	for name, token := range hiddenTokens(t) {
		for _, path := range closedPaths {
			if rec := f.get(t, path, token); rec.Code != http.StatusNotFound {
				t.Errorf("%s, %s: код %d, ждали 404", name, path, rec.Code)
			}
		}
		for _, path := range []string{"/api/works/1/pages/1003", "/api/works/1/pages/1007/render", "/api/works/1/pages/by-number/7"} {
			if rec := f.get(t, path, token); rec.Code != http.StatusOK {
				t.Errorf("%s, открытая %s: код %d", name, path, rec.Code)
			}
		}
		rec := f.get(t, "/api/works/1/pages", token)
		if strings.Contains(rec.Body.String(), "Полоса 5.") || strings.Contains(rec.Body.String(), "page_6.png") {
			t.Errorf("%s: список полос тома несёт закрытую полосу", name)
		}
	}
	for name, token := range staffTokens(t) {
		for _, path := range []string{"/api/works/1/pages/1005", "/api/works/1/pages/1006/render", "/api/works/1/pages/by-number/5"} {
			if rec := f.get(t, path, token); rec.Code != http.StatusOK {
				t.Errorf("%s, %s: код %d, ждали 200", name, path, rec.Code)
			}
		}
	}
}

func TestPageMapOmitsClosed(t *testing.T) {
	f := newClosedChaptersFixture(t)
	nums := func(rec *httptest.ResponseRecorder) []int {
		var entries []models.PageMapEntry
		_ = json.Unmarshal(rec.Body.Bytes(), &entries)
		return mapNums(entries)
	}
	want := []int{1, 2, 3, 7, 8, 9, 10, 13, 14, 15, 16, 17, 18, 19, 20}
	for name, token := range hiddenTokens(t) {
		if got := nums(f.get(t, "/api/works/1/page-map", token)); !slices.Equal(got, want) {
			t.Errorf("%s: карта %v, ждали %v", name, got, want)
		}
	}
	for name, token := range staffTokens(t) {
		if got := nums(f.get(t, "/api/works/1/page-map", token)); len(got) != 20 {
			t.Errorf("%s: карта %v, ждали все 20", name, got)
		}
	}
}

func decodeWindow(t *testing.T, rec *httptest.ResponseRecorder) ReadingWindowResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	var body ReadingWindowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func windowNums(b ReadingWindowResponse) []int {
	out := make([]int, len(b.Pages))
	for i, p := range b.Pages {
		out[i] = p.PageNumber
	}
	return out
}

// Поток идёт сквозь закрытое: окно добирает открытые полосы за дырой, а не
// встаёт перед ней, — работа с закрытой главой читается дальше.
func TestReadingWindowOmitsClosed(t *testing.T) {
	f := newClosedChaptersFixture(t)
	for name, token := range hiddenTokens(t) {
		b := decodeWindow(t, f.get(t, "/api/works/1/reading?from=1&count=5", token))
		if got := windowNums(b); !slices.Equal(got, []int{1, 2, 3, 7, 8}) {
			t.Errorf("%s: окно %v, ждали [1 2 3 7 8]", name, got)
		}
		if b.NextFrom == nil || *b.NextFrom != 9 {
			t.Errorf("%s: next_from = %v, ждали 9", name, b.NextFrom)
		}
		b = decodeWindow(t, f.get(t, "/api/works/1/reading?from=9&count=5", token))
		if got := windowNums(b); !slices.Equal(got, []int{9, 10, 13, 14, 15}) {
			t.Errorf("%s: второе окно %v, ждали [9 10 13 14 15]", name, got)
		}
		if b.NextFrom == nil || *b.NextFrom != 16 {
			t.Errorf("%s: next_from = %v, ждали 16", name, b.NextFrom)
		}
		// Вход посреди закрытого — с первой открытой полосы за ним.
		b = decodeWindow(t, f.get(t, "/api/works/1/reading?from=5&count=2", token))
		if got := windowNums(b); !slices.Equal(got, []int{7, 8}) {
			t.Errorf("%s: окно с закрытой полосы %v, ждали [7 8]", name, got)
		}
		if strings.Contains(f.get(t, "/api/works/1/reading?from=1&count=50", token).Body.String(), "Полоса 5.") {
			t.Errorf("%s: поток несёт закрытую полосу", name)
		}
	}
	for name, token := range staffTokens(t) {
		b := decodeWindow(t, f.get(t, "/api/works/1/reading?from=1&count=5", token))
		if got := windowNums(b); !slices.Equal(got, []int{1, 2, 3, 4, 5}) {
			t.Errorf("%s: окно %v", name, got)
		}
	}
}

// Родитель открыт, но его диапазон теряет полосы закрытой подглавы.
func TestParentChapterPagesOmitClosedChild(t *testing.T) {
	f := newClosedChaptersFixture(t)
	for name, token := range hiddenTokens(t) {
		b := decodeChapterPages(t, f.get(t, "/api/works/1/chapters/4/pages", token))
		if got := chapterPageNums(b.Pages); !slices.Equal(got, []int{10, 13, 14, 15, 16, 17, 18, 19, 20}) {
			t.Errorf("%s: полосы родителя %v", name, got)
		}
	}
	for name, token := range staffTokens(t) {
		b := decodeChapterPages(t, f.get(t, "/api/works/1/chapters/4/pages", token))
		if got := chapterPageNums(b.Pages); len(got) != 11 {
			t.Errorf("%s: полосы родителя %v, ждали все 11", name, got)
		}
	}
}

// Review Focus 1: сотрудник прогрел соседку, чья крайняя полоса общая с
// закрытой главой; аноним по тому же адресу получает соседку без общей
// полосы, а не файл сотрудника. Файла диапазона с закрытым нет вовсе, а
// открытая глава кэшируется по-прежнему.
func TestStaffWarmedNeighborNotServedWithSharedPage(t *testing.T) {
	f := newClosedChaptersFixture(t)
	path := "/api/works/1/chapters/2/pages"
	staff := decodeChapterPages(t, f.get(t, path, editorToken(t)))
	if got := chapterPageNums(staff.Pages); !slices.Equal(got, []int{6, 7, 8, 9}) {
		t.Fatalf("сотрудник: %v, ждали [6 7 8 9]", got)
	}
	for name, token := range hiddenTokens(t) {
		rec := f.get(t, path, token)
		b := decodeChapterPages(t, rec)
		if got := chapterPageNums(b.Pages); !slices.Equal(got, []int{7, 8, 9}) {
			t.Errorf("%s: %v, ждали [7 8 9]", name, got)
		}
		if strings.Contains(rec.Body.String(), "Полоса 6.") {
			t.Errorf("%s: общая полоса в ответе", name)
		}
	}
	if _, _, ok := f.cache.Open(pagecache.Key{WorkID: 1, Start: 6, End: 9}); ok {
		t.Error("диапазон с закрытой полосой лёг в кэш")
	}
	f.get(t, "/api/works/1/chapters/1/pages", "")
	if _, _, ok := f.cache.Open(pagecache.Key{WorkID: 1, Start: 1, End: 3}); !ok {
		t.Error("открытая глава не легла в кэш: проверка выше ничего не доказывает")
	}
}

func TestStaffSeesClosedChapterMarker(t *testing.T) {
	f := newClosedChaptersFixture(t)
	for name, token := range staffTokens(t) {
		b := decodeChapterPages(t, f.get(t, "/api/works/1/chapters/2/pages", token))
		marks := map[int]string{}
		for _, p := range b.Pages {
			marks[p.PageNumber] = p.ClosedChapter
		}
		if marks[6] != "Закрытая" || marks[7] != "" {
			t.Errorf("%s: пометки главы %v", name, marks)
		}
		b = decodeChapterPages(t, f.get(t, "/api/works/1/chapters/4/pages", token))
		for _, p := range b.Pages {
			want := ""
			if p.PageNumber == 11 || p.PageNumber == 12 {
				want = "Речь"
			}
			if p.ClosedChapter != want {
				t.Errorf("%s: полоса %d родителя — %q, ждали %q", name, p.PageNumber, p.ClosedChapter, want)
			}
		}
		w := decodeWindow(t, f.get(t, "/api/works/1/reading?from=3&count=4", token))
		got := map[int]string{}
		for _, p := range w.Pages {
			got[p.PageNumber] = p.ClosedChapter
		}
		if got[3] != "" || got[4] != "Закрытая" || got[5] != "Закрытая" || got[6] != "Закрытая" {
			t.Errorf("%s: пометки потока %v", name, got)
		}
	}
	for name, token := range hiddenTokens(t) {
		for _, path := range []string{"/api/works/1/chapters/2/pages", "/api/works/1/reading?from=1&count=20"} {
			if body := f.get(t, path, token).Body.String(); strings.Contains(body, "closed_chapter") {
				t.Errorf("%s, %s: пометка закрытой главы", name, path)
			}
		}
	}
}

func TestSuggestionOnClosedPage404ForReader(t *testing.T) {
	f := newClosedChaptersFixture(t)
	body := func(n int) map[string]any {
		return map[string]any{
			"proposed_markdown": "Поправленный текст",
			"note":              "опечатка",
			"base_sha256":       pageSHA256(fmt.Sprintf("Полоса %d.", n)),
		}
	}
	reader := readerToken(t)
	if rec := f.do(t, http.MethodPost, "/api/works/1/pages/1005/suggestions", reader, body(5)); rec.Code != http.StatusNotFound {
		t.Errorf("закрытая полоса: код %d, ждали 404: %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, http.MethodPost, "/api/works/1/pages/1006/suggestions", reader, body(6)); rec.Code != http.StatusNotFound {
		t.Errorf("общая полоса: код %d, ждали 404", rec.Code)
	}
	if rec := f.do(t, http.MethodPost, "/api/works/1/pages/1007/suggestions", reader, body(7)); rec.Code != http.StatusCreated {
		t.Errorf("открытая полоса: код %d, ждали 201: %s", rec.Code, rec.Body)
	}
}

func fileURLOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, rec.Body)
	}
	var w struct {
		FileURL string `json:"file_url"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &w)
	return w.FileURL
}

func TestFileURLHiddenWithClosedChapter(t *testing.T) {
	f := newClosedChaptersFixture(t)
	for name, token := range hiddenTokens(t) {
		if u := fileURLOf(t, f.get(t, "/api/works/1", token)); u != "" {
			t.Errorf("%s: file_url тома с закрытой главой = %q", name, u)
		}
		if body := f.get(t, "/api/works", token).Body.String(); strings.Contains(body, "a.pdf") {
			t.Errorf("%s: список работ несёт оригинал тома с закрытой главой", name)
		}
	}
	for name, token := range staffTokens(t) {
		if u := fileURLOf(t, f.get(t, "/api/works/1", token)); !strings.Contains(u, "a.pdf") {
			t.Errorf("%s: file_url = %q, ждали оригинал", name, u)
		}
	}
}

func TestFileURLKeptWithoutClosed(t *testing.T) {
	f := newClosedChaptersFixture(t)
	for name, token := range hiddenTokens(t) {
		if u := fileURLOf(t, f.get(t, "/api/works/2", token)); !strings.Contains(u, "b.pdf") {
			t.Errorf("%s: file_url тома без закрытого = %q", name, u)
		}
		if body := f.get(t, "/api/works", token).Body.String(); !strings.Contains(body, "b.pdf") {
			t.Errorf("%s: список работ потерял оригинал тома без закрытого", name)
		}
	}
}

// Перенос решения задачи 3: обработчик глав, собранный как в NewServer
// (голое хранилище + WithClosedChapters), читает читателю через обёртку, а
// охрана записи видит закрытое и на контексте без claims. Иначе удаление
// родителя закрытой подглавы прошло бы охрану и унесло подглаву каскадом.
func TestClosedChapterWiredHandlerKeepsWriteGuard(t *testing.T) {
	deletes := 0
	raw := closedChapterStore(&deletes)
	spans := closedChapterSpans()
	h := NewChapterHandler(raw, NewViewPages(memPageStore(numberedPages(1, 20)), spans),
		markdown.NewRenderer(), nil).WithClosedChapters(spans)

	if rec := doChapter(t, h.Get, http.MethodGet, 3, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("Get закрытой без claims: код %d — обёртка чтения не подключена", rec.Code)
	}
	want409(t, doChapter(t, h.Delete, http.MethodDelete, 4, nil), &deletes)
	want409(t, doChapter(t, h.Delete, http.MethodDelete, 3, nil), &deletes)
	want409(t, doChapter(t, h.Update, http.MethodPut, 31, updBody("x", 5, 5, i64(3))), &deletes)
}

// Обработчик глав строит обёртку сам: обёрнутое хранилище в конструкторе
// спрятало бы закрытое от охраны записи, поэтому оно отвергается сразу.
func TestChapterHandlerRefusesWrappedStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewChapterHandler принял ViewChapters")
		}
	}()
	deletes := 0
	NewChapterHandler(NewViewChapters(closedChapterStore(&deletes), closedChapterSpans()), &fakePageStore{}, nil, nil)
}

// Сотрудник правит закрытую полосу как прежде: пути записи перечитывают
// полосу на контексте запроса, а не на голом context.Background(), иначе
// обёртка спрятала бы полосу и от редактора.
func TestStaffWritesClosedPage(t *testing.T) {
	f := newClosedChaptersFixture(t)
	editor := editorToken(t)
	rec := f.do(t, http.MethodPut, "/api/works/1/pages/1005", editor,
		map[string]any{"content_markdown": "Полоса 5, правка.", "status": models.PageStatusProofread})
	if rec.Code != http.StatusOK {
		t.Errorf("правка закрытой полосы редактором: код %d: %s", rec.Code, rec.Body)
	}
	rec = f.do(t, http.MethodPost, "/api/suggestions/3/accept", editor,
		map[string]any{"content_markdown": "Принятый текст."})
	if rec.Code != http.StatusOK {
		t.Errorf("приём предложения на закрытой полосе: код %d: %s", rec.Code, rec.Body)
	}
}

// Пункт подборки на соседке с общей полосой: тот же диапазон, что у главы, и
// тот же кэш — прогрев сотрудником не уходит анониму, файла нет вовсе.
func TestCollectionItemSharedPageNotServedFromStaffCache(t *testing.T) {
	store := collectionFixture()
	row := chapterRow(11, 1, 2, 1)
	row.ChapterStartPage, row.ChapterEndPage = ptrInt(6), ptrInt(9)
	store.rows = []repository.ItemRow{row}
	store.item = &models.CollectionItem{ID: 11, CollectionID: 1, Kind: models.CollectionItemKindChapter,
		ChapterID: ptrInt64(2), WorkID: ptrInt64(1)}
	cache := pagecache.New(t.TempDir())
	pages := NewViewPages(memPageStore(numberedPages(1, 20)), closedChapterSpans())
	h := NewCollectionHandler(store, pages, markdown.NewRenderer(), NewRangeCache(cache), "", false).
		WithClosed(&fakeClosedStore{})

	staff := httptest.NewRecorder()
	withClaims(editorClaims(), h.ItemPages)(staff, itemPagesRequestFor("materializm", "11"))
	if got := chapterPageNums(decodeChapterPages(t, staff).Pages); !slices.Equal(got, []int{6, 7, 8, 9}) {
		t.Fatalf("сотрудник: %v", got)
	}
	rec := httptest.NewRecorder()
	h.ItemPages(rec, itemPagesRequestFor("materializm", "11"))
	if got := chapterPageNums(decodeChapterPages(t, rec).Pages); !slices.Equal(got, []int{7, 8, 9}) {
		t.Errorf("аноним: %v, ждали [7 8 9]", got)
	}
	if _, _, ok := cache.Open(pagecache.Key{WorkID: 1, Start: 6, End: 9}); ok {
		t.Error("диапазон с закрытой полосой лёг в кэш")
	}
}
