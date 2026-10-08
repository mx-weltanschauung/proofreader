package export

import (
	"testing"

	"proofreader/internal/models"
)

func TestPaths(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"index", worksIndexPath(), "works.md"},
		{"work", workFilePath(1), "works/0001/work.md"},
		{"chapters", chaptersFilePath(1), "works/0001/chapters.md"},
		{"page", pageFilePath(1, 7), "works/0001/pages/0007.md"},
		{"four-digit page", pageFilePath(23, 1234), "works/0023/pages/1234.md"},
		{"five-digit page keeps its length", pageFilePath(1, 12345), "works/0001/pages/12345.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("= %q, want %q", c.got, c.want)
			}
		})
	}
}

func TestSortWorksByID(t *testing.T) {
	in := []*models.Work{{ID: 10}, {ID: 2}, {ID: 7}}

	got := sortWorks(in)

	want := []int64{2, 7, 10}
	for i, w := range got {
		if w.ID != want[i] {
			t.Fatalf("sortWorks() ids = %v..., want %v", got[i].ID, want)
		}
	}
	if in[0].ID != 10 {
		t.Error("sortWorks must not reorder the caller's slice")
	}
}

func TestSortPagesByNumber(t *testing.T) {
	in := []*models.Page{{ID: 3, PageNumber: 12}, {ID: 1, PageNumber: 2}, {ID: 2, PageNumber: 2}}

	got := sortPages(in)

	if got[0].ID != 1 || got[1].ID != 2 || got[2].PageNumber != 12 {
		t.Errorf("sortPages() = %+v, want page 2 (id 1), page 2 (id 2), page 12", got)
	}
}
