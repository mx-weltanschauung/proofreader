package export

import (
	"fmt"
	"sort"

	"proofreader/internal/models"
)

// pad4 zero-pads to four digits so names sort lexicographically the way they
// sort numerically. Longer numbers simply keep their length.
func pad4(n int64) string {
	return fmt.Sprintf("%04d", n)
}

func worksIndexPath() string {
	return "works.md"
}

func workDir(workID int64) string {
	return "works/" + pad4(workID)
}

func workFilePath(workID int64) string {
	return workDir(workID) + "/work.md"
}

func chaptersFilePath(workID int64) string {
	return workDir(workID) + "/chapters.md"
}

func pageFilePath(workID int64, pageNumber int) string {
	return workDir(workID) + "/pages/" + pad4(int64(pageNumber)) + ".md"
}

// sortWorks returns works ordered by id, without touching the input slice —
// the caller's slice belongs to the repository layer.
func sortWorks(works []*models.Work) []*models.Work {
	out := make([]*models.Work, len(works))
	copy(out, works)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// sortPages returns pages ordered by page number, with id breaking ties so the
// output cannot wobble between runs if two rows share a number.
func sortPages(pages []*models.Page) []*models.Page {
	out := make([]*models.Page, len(pages))
	copy(out, pages)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PageNumber != out[j].PageNumber {
			return out[i].PageNumber < out[j].PageNumber
		}
		return out[i].ID < out[j].ID
	})
	return out
}
