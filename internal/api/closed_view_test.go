package api

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"proofreader/internal/auth"
	"proofreader/internal/middleware"
	"proofreader/internal/models"
)

// spanStore — ClosedSpansStore над готовыми диапазонами по работам.
type spanStore map[int64][]models.PageSpan

func (s spanStore) ClosedSpans(_ context.Context, workID int64) ([]models.PageSpan, error) {
	return s[workID], nil
}

func (s spanStore) ClosedChapterIDs(_ context.Context, _ int64) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}

func roleCtx(role models.UserRole) context.Context {
	return context.WithValue(context.Background(), middleware.UserContextKey,
		&auth.Claims{UserID: 1, Role: role})
}

// viewers — кто закрытого не видит: аноним без claims и читатель.
func hiddenViewers() map[string]context.Context {
	return map[string]context.Context{
		"аноним":   context.Background(),
		"читатель": roleCtx(models.RoleReader),
	}
}

func staffViewers() map[string]context.Context {
	return map[string]context.Context{
		"редактор":      roleCtx(models.RoleEditor),
		"администратор": roleCtx(models.RoleAdministrator),
	}
}

// memPages — полосы 1..n работы workID, id полосы = workID*1000 + номер.
func memPages(workID int64, n int) []*models.Page {
	out := make([]*models.Page, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, &models.Page{ID: workID*1000 + int64(i), WorkID: workID, PageNumber: i,
			ContentMarkdown: "Полоса.", Status: models.PageStatusNotProofread})
	}
	return out
}

// memPageStore — fakePageStore над набором полос нескольких работ.
func memPageStore(all ...[]*models.Page) *fakePageStore {
	byWork := map[int64][]*models.Page{}
	byID := map[int64]*models.Page{}
	for _, ps := range all {
		for _, p := range ps {
			byWork[p.WorkID] = append(byWork[p.WorkID], p)
			byID[p.ID] = p
		}
	}
	notFound := func() error { return errHiddenPage }
	return &fakePageStore{
		getByIDFn: func(_ context.Context, id int64) (*models.Page, error) {
			if p, ok := byID[id]; ok {
				return p, nil
			}
			return nil, notFound()
		},
		getByWorkAndPageNumberFn: func(_ context.Context, w int64, n int) (*models.Page, error) {
			for _, p := range byWork[w] {
				if p.PageNumber == n {
					return p, nil
				}
			}
			return nil, notFound()
		},
		getPageRangeFn: func(_ context.Context, w int64, s, e int) ([]*models.Page, error) {
			var out []*models.Page
			for _, p := range byWork[w] {
				if p.PageNumber >= s && p.PageNumber <= e {
					out = append(out, p)
				}
			}
			return out, nil
		},
		getPagesByNumbersFn: func(_ context.Context, w int64, ns []int) ([]*models.Page, error) {
			var out []*models.Page
			for _, p := range byWork[w] {
				if slices.Contains(ns, p.PageNumber) {
					out = append(out, p)
				}
			}
			return out, nil
		},
		listByWorkFn: func(_ context.Context, w int64) ([]*models.Page, error) {
			return byWork[w], nil
		},
		listPageMapFn: func(_ context.Context, w int64) ([]models.PageMapEntry, error) {
			var out []models.PageMapEntry
			for _, p := range byWork[w] {
				out = append(out, models.PageMapEntry{PageNumber: p.PageNumber, Status: p.Status})
			}
			return out, nil
		},
		maxPageNumberFn: func(_ context.Context, w int64) (int, error) {
			return len(byWork[w]), nil
		},
	}
}

func pageNums(pages []*models.Page) []int {
	out := make([]int, len(pages))
	for i, p := range pages {
		out[i] = p.PageNumber
	}
	return out
}

func mapNums(entries []models.PageMapEntry) []int {
	out := make([]int, len(entries))
	for i, e := range entries {
		out[i] = e.PageNumber
	}
	return out
}

// Работа 1: полосы 1..10, закрыта глава на 4..6 (6 — общая с соседкой
// 6..9). Работа 2: ничего не закрыто. Работа 3: закрыт хвост 8..10.
func viewPagesFixture() *ViewPages {
	inner := memPageStore(memPages(1, 10), memPages(2, 10), memPages(3, 10))
	return NewViewPages(inner, spanStore{1: {{Start: 4, End: 6}}, 3: {{Start: 8, End: 10}}})
}

func TestViewPagesHidesClosedFromAnonymousAndReader(t *testing.T) {
	v := viewPagesFixture()
	open := []int{1, 2, 3, 7, 8, 9, 10}
	for name, ctx := range hiddenViewers() {
		for _, id := range []int64{1005, 1006, 1004} {
			if p, err := v.GetByID(ctx, id); err == nil || err.Error() != "page not found" {
				t.Errorf("%s: GetByID(%d) = %v, %v; ждали «page not found»", name, id, p, err)
			}
		}
		if p, err := v.GetByID(ctx, 1003); err != nil || p.PageNumber != 3 {
			t.Errorf("%s: открытая полоса GetByID = %v, %v", name, p, err)
		}
		if p, err := v.GetByWorkAndPageNumber(ctx, 1, 6); err == nil || err.Error() != "page not found" {
			t.Errorf("%s: GetByWorkAndPageNumber(1, 6) = %v, %v", name, p, err)
		}
		if p, err := v.GetByWorkAndPageNumber(ctx, 1, 7); err != nil || p.PageNumber != 7 {
			t.Errorf("%s: GetByWorkAndPageNumber(1, 7) = %v, %v", name, p, err)
		}
		if got, _ := v.GetPageRange(ctx, 1, 1, 10); !slices.Equal(pageNums(got), open) {
			t.Errorf("%s: GetPageRange = %v, ждали %v", name, pageNums(got), open)
		}
		if got, _ := v.GetPagesByNumbers(ctx, 1, []int{3, 4, 6, 7}); !slices.Equal(pageNums(got), []int{3, 7}) {
			t.Errorf("%s: GetPagesByNumbers = %v, ждали [3 7]", name, pageNums(got))
		}
		if got, _ := v.ListByWork(ctx, 1); !slices.Equal(pageNums(got), open) {
			t.Errorf("%s: ListByWork = %v", name, pageNums(got))
		}
		if got, _ := v.ListPageMap(ctx, 1); !slices.Equal(mapNums(got), open) {
			t.Errorf("%s: ListPageMap = %v", name, mapNums(got))
		}
		if n, _ := v.MaxPageNumber(ctx, 1); n != 10 {
			t.Errorf("%s: MaxPageNumber(1) = %d, ждали 10", name, n)
		}
		if n, _ := v.MaxPageNumber(ctx, 3); n != 7 {
			t.Errorf("%s: MaxPageNumber(3) с закрытым хвостом = %d, ждали 7", name, n)
		}
		if got, _ := v.ListByWork(ctx, 2); len(got) != 10 {
			t.Errorf("%s: работа без закрытого — %d полос", name, len(got))
		}
	}
}

func TestViewPagesStaffSeesAll(t *testing.T) {
	v := viewPagesFixture()
	for name, ctx := range staffViewers() {
		if p, err := v.GetByID(ctx, 1005); err != nil || p.PageNumber != 5 {
			t.Errorf("%s: GetByID закрытой = %v, %v", name, p, err)
		}
		if p, err := v.GetByWorkAndPageNumber(ctx, 1, 6); err != nil || p.PageNumber != 6 {
			t.Errorf("%s: GetByWorkAndPageNumber закрытой = %v, %v", name, p, err)
		}
		if got, _ := v.GetPageRange(ctx, 1, 1, 10); len(got) != 10 {
			t.Errorf("%s: GetPageRange = %v", name, pageNums(got))
		}
		if got, _ := v.GetPagesByNumbers(ctx, 1, []int{3, 4, 6, 7}); len(got) != 4 {
			t.Errorf("%s: GetPagesByNumbers = %v", name, pageNums(got))
		}
		if got, _ := v.ListByWork(ctx, 1); len(got) != 10 {
			t.Errorf("%s: ListByWork = %v", name, pageNums(got))
		}
		if got, _ := v.ListPageMap(ctx, 1); len(got) != 10 {
			t.Errorf("%s: ListPageMap = %v", name, mapNums(got))
		}
		if n, _ := v.MaxPageNumber(ctx, 3); n != 10 {
			t.Errorf("%s: MaxPageNumber(3) = %d", name, n)
		}
	}
}

func TestViewPagesHasClosedInIgnoresViewer(t *testing.T) {
	v := viewPagesFixture()
	for name, ctx := range map[string]context.Context{"аноним": context.Background(), "редактор": roleCtx(models.RoleEditor)} {
		for _, c := range []struct {
			work       int64
			start, end int
			want       bool
		}{{1, 1, 3, false}, {1, 6, 9, true}, {1, 1, 4, true}, {1, 7, 10, false}, {2, 1, 10, false}} {
			got, err := v.HasClosedIn(ctx, c.work, c.start, c.end)
			if err != nil || got != c.want {
				t.Errorf("%s: HasClosedIn(%d, %d, %d) = %v, %v; ждали %v", name, c.work, c.start, c.end, got, err, c.want)
			}
		}
	}
}

func TestViewPagesWritesPassThrough(t *testing.T) {
	inner := memPageStore(memPages(1, 10))
	created := 0
	inner.createFn = func(context.Context, *models.Page) error { created++; return nil }
	v := NewViewPages(inner, spanStore{1: {{Start: 1, End: 10}}})
	ctx := context.Background()
	if err := v.Create(ctx, &models.Page{WorkID: 1, PageNumber: 5}); err != nil || created != 1 {
		t.Errorf("Create: %v, вызовов %d", err, created)
	}
	if err := v.SaveEdit(ctx, &models.Page{ID: 1005}, &models.PageVersion{}); err != nil || len(inner.savedVersions) != 1 {
		t.Errorf("SaveEdit: %v, снимков %d", err, len(inner.savedVersions))
	}
}

// fakeChapterReader — fakeChapterStore с главой по полосе.
type fakeChapterReader struct {
	*fakeChapterStore
	findByPageFn func(ctx context.Context, workID int64, page int) (*models.Chapter, error)
}

func (f *fakeChapterReader) FindByPage(ctx context.Context, workID int64, page int) (*models.Chapter, error) {
	if f.findByPageFn != nil {
		return f.findByPageFn(ctx, workID, page)
	}
	return nil, nil
}

// viewChaptersFixture — работа 1: 1 «Открытая» 1..3; 3 «Закрытая» 4..6
// (своим признаком) с подглавой 31 (закрыта через родителя); 2 «Соседка»
// 6..9 (6 — общая); 4 «Родитель» 10..20 с закрытой подглавой 41 11..12,
// открытой 42 13..14 и открытой 43 12..12 — она уже 41, поэтому самая узкая
// глава закрытой полосы 12 открыта.
func viewChaptersFixture() *ViewChapters {
	return viewChaptersOver(spanStore{1: {{Start: 4, End: 6}, {Start: 5, End: 5}, {Start: 11, End: 12}}})
}

func viewChaptersOver(closed ClosedSpansStore) *ViewChapters {
	flat := func() map[int64]*models.Chapter {
		return map[int64]*models.Chapter{
			1:  {ID: 1, WorkID: 1, Title: "Открытая", StartPage: 1, EndPage: 3},
			3:  {ID: 3, WorkID: 1, Title: "Закрытая", StartPage: 4, EndPage: 6, Closed: true, ClosedBy: "chapter"},
			31: {ID: 31, WorkID: 1, Title: "Подглава закрытой", StartPage: 5, EndPage: 5, ParentID: i64(3), ClosedBy: "parent"},
			2:  {ID: 2, WorkID: 1, Title: "Соседка", StartPage: 6, EndPage: 9},
			4:  {ID: 4, WorkID: 1, Title: "Родитель", StartPage: 10, EndPage: 20},
			41: {ID: 41, WorkID: 1, Title: "Речь", StartPage: 11, EndPage: 12, ParentID: i64(4), Closed: true, ClosedBy: "chapter"},
			42: {ID: 42, WorkID: 1, Title: "Прения", StartPage: 13, EndPage: 14, ParentID: i64(4)},
			43: {ID: 43, WorkID: 1, Title: "Реплика", StartPage: 12, EndPage: 12, ParentID: i64(4)},
		}
	}
	inner := &fakeChapterReader{
		fakeChapterStore: &fakeChapterStore{
			getByIDFn: func(_ context.Context, id int64) (*models.Chapter, error) {
				if c, ok := flat()[id]; ok {
					return c, nil
				}
				return nil, errHiddenChapter
			},
			listHierarchicalFn: func(_ context.Context, _ int64) ([]*models.Chapter, error) {
				m := flat()
				m[3].Children = []*models.Chapter{m[31]}
				m[4].Children = []*models.Chapter{m[41], m[42], m[43]}
				return []*models.Chapter{m[1], m[3], m[2], m[4]}, nil
			},
		},
		findByPageFn: func(_ context.Context, _ int64, page int) (*models.Chapter, error) {
			// Самая узкая накрывающая, как в репозитории.
			var best *models.Chapter
			for _, c := range flat() {
				if page < c.StartPage || page > c.EndPage {
					continue
				}
				if best == nil || c.EndPage-c.StartPage < best.EndPage-best.StartPage ||
					(c.EndPage-c.StartPage == best.EndPage-best.StartPage && c.ID < best.ID) {
					best = c
				}
			}
			return best, nil
		},
	}
	return NewViewChapters(inner, closed)
}

func treeIDs(tree []*models.Chapter) []int64 {
	var out []int64
	var walk func([]*models.Chapter)
	walk = func(cs []*models.Chapter) {
		for _, c := range cs {
			out = append(out, c.ID)
			walk(c.Children)
		}
	}
	walk(tree)
	return out
}

func TestViewChaptersHidesClosedSubtree(t *testing.T) {
	v := viewChaptersFixture()
	for name, ctx := range hiddenViewers() {
		tree, err := v.ListByWorkHierarchical(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := treeIDs(tree), []int64{1, 2, 4, 42, 43}; !slices.Equal(got, want) {
			t.Errorf("%s: дерево %v, ждали %v", name, got, want)
		}
		for _, id := range []int64{3, 31, 41} {
			if c, err := v.GetByID(ctx, id); err == nil || err.Error() != "chapter not found" {
				t.Errorf("%s: GetByID(%d) = %v, %v; ждали «chapter not found»", name, id, c, err)
			}
		}
		if c, err := v.GetByID(ctx, 2); err != nil || c.ID != 2 {
			t.Errorf("%s: GetByID открытой = %v, %v", name, c, err)
		}
		for _, page := range []int{5, 6, 11, 12} {
			if c, err := v.FindByPage(ctx, 1, page); err != nil || c != nil {
				t.Errorf("%s: FindByPage(%d) = %v, %v; ждали nil — полосы нет", name, page, c, err)
			}
		}
		if c, err := v.FindByPage(ctx, 1, 7); err != nil || c == nil || c.ID != 2 {
			t.Errorf("%s: FindByPage(7) = %v, %v; ждали соседку", name, c, err)
		}
		if c, err := v.FindByPage(ctx, 1, 13); err != nil || c == nil || c.ID != 42 {
			t.Errorf("%s: FindByPage(13) = %v, %v", name, c, err)
		}
	}
	for name, ctx := range staffViewers() {
		tree, _ := v.ListByWorkHierarchical(ctx, 1)
		if got, want := treeIDs(tree), []int64{1, 3, 31, 2, 4, 41, 42, 43}; !slices.Equal(got, want) {
			t.Errorf("%s: дерево %v, ждали %v", name, got, want)
		}
		if c, err := v.GetByID(ctx, 31); err != nil || c.ID != 31 {
			t.Errorf("%s: GetByID(31) = %v, %v", name, c, err)
		}
		if c, err := v.FindByPage(ctx, 1, 5); err != nil || c == nil || c.ID != 31 {
			t.Errorf("%s: FindByPage(5) = %v, %v", name, c, err)
		}
	}
}

// Метод чтения, добавленный в PageStore или ChapterReadStore, обязан
// получить решение здесь: фильтруется он или это запись. Обёртки интерфейс не
// встраивают, поэтому без реализации новый метод не скомпилируется, а без
// строки в этих списках — роняет этот тест.
func TestViewWrappersClassifyEveryMethod(t *testing.T) {
	cases := []struct {
		name            string
		iface           reflect.Type
		filtered, write []string
	}{
		{"PageStore", reflect.TypeOf((*PageStore)(nil)).Elem(),
			[]string{"GetByID", "GetByWorkAndPageNumber", "GetPageRange", "GetPagesByNumbers",
				"ListByWork", "ListPageMap", "MaxPageNumber"},
			[]string{"Create", "SaveEdit"}},
		{"ChapterReadStore", reflect.TypeOf((*ChapterReadStore)(nil)).Elem(),
			[]string{"GetByID", "ListByWorkHierarchical", "FindByPage"},
			[]string{"Create", "Delete", "Move", "Update"}},
	}
	for _, c := range cases {
		for i := 0; i < c.iface.NumMethod(); i++ {
			m := c.iface.Method(i).Name
			f, w := slices.Contains(c.filtered, m), slices.Contains(c.write, m)
			if f == w {
				t.Errorf("%s.%s: метод не разобран (фильтр %v, запись %v) — реши, прячет ли он закрытое",
					c.name, m, f, w)
			}
		}
		if got := len(c.filtered) + len(c.write); got != c.iface.NumMethod() {
			t.Errorf("%s: в списках %d методов, в интерфейсе %d", c.name, got, c.iface.NumMethod())
		}
	}
}

// Заглавие закрытой главы для пометки сотруднику: самая широкая из
// накрывающих (родитель, а не подглава, закрытая вместе с ним), вне
// закрытого — пусто.
func TestClosedMarkersNameOwnClosedChapter(t *testing.T) {
	v := viewChaptersFixture()
	mark, err := closedMarkers(roleCtx(models.RoleEditor), v, 1)
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range map[int]string{3: "", 4: "Закрытая", 5: "Закрытая", 6: "Закрытая", 7: "", 11: "Речь", 13: ""} {
		if got := mark(page); got != want {
			t.Errorf("полоса %d: %q, ждали %q", page, got, want)
		}
	}
}

// Без хранилища диапазонов закрытая глава скрыта всё равно — по своей строке.
func TestViewChaptersHidesClosedRowWithoutSpans(t *testing.T) {
	v := viewChaptersOver(nil)
	ctx := context.Background()
	if c, err := v.FindByPage(ctx, 1, 5); err != nil || c != nil {
		t.Errorf("FindByPage(5) = %v, %v; ждали nil", c, err)
	}
	if c, err := v.GetByID(ctx, 3); err == nil {
		t.Errorf("GetByID(3) = %v, ждали отказ", c)
	}
	if c, err := v.FindByPage(ctx, 1, 13); err != nil || c == nil || c.ID != 42 {
		t.Errorf("FindByPage(13) = %v, %v", c, err)
	}
}
