package staticsite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proofreader/internal/models"
	"proofreader/pkg/book"
)

type fakeSource struct {
	shelf       *models.Shelf
	highlights  []models.EditionHighlight
	works       map[int64]*models.Work
	front       map[int64][]*models.Work
	volumes     map[int64]*book.Book
	conceptList []ConceptEntry
	concepts    map[string]*Concept
	collections []Collection
	documents   []Document
}

func (f *fakeSource) Shelf(context.Context) (*models.Shelf, error) { return f.shelf, nil }
func (f *fakeSource) Highlights(context.Context) ([]models.EditionHighlight, error) {
	return f.highlights, nil
}
func (f *fakeSource) Work(_ context.Context, id int64) (*models.Work, error) { return f.works[id], nil }
func (f *fakeSource) FrontMatter(_ context.Context, id int64) ([]*models.Work, error) {
	return f.front[id], nil
}
func (f *fakeSource) Volume(_ context.Context, id int64) (*book.Book, error) {
	return f.volumes[id], nil
}
func (f *fakeSource) ConceptList(context.Context) ([]ConceptEntry, error) { return f.conceptList, nil }
func (f *fakeSource) Concept(_ context.Context, slug string) (*Concept, error) {
	return f.concepts[slug], nil
}
func (f *fakeSource) Collections(context.Context) ([]Collection, error) { return f.collections, nil }
func (f *fakeSource) Documents(context.Context) ([]Document, error)     { return f.documents, nil }

func intp(v int) *int { return &v }

func page(internal, printed int, text string) book.Page {
	return book.Page{Internal: internal, Printed: printed, HTML: "<p>" + text + "</p>"}
}

// fixture — издание Сталина: том 1 (работа 100) с безымянным началом (с. 1),
// главой 10 с подглавой 11 (с. 2—3) и главой 12 с опасным заглавием (с. 4);
// передние листы тома (работа 103); том 2 (работа 101) без полос по сводке
// и том 3 (работа 102) с полосами по сводке, но без книги.
func fixture() *fakeSource {
	ed := &models.Edition{ID: 7, Title: "И. В. Сталин. Сочинения", Slug: "stalin-13", URLSlug: "stalin"}
	vol := func(id int64, n, pages int) *models.VolumeSummary {
		return &models.VolumeSummary{
			Work: models.Work{ID: id, Title: "И. В. Сталин. Сочинения. Том " + string(rune('0'+n)),
				Slug: "stalin-t0" + string(rune('0'+n)), VolumeNumber: intp(n), EditionID: &ed.ID},
			PagesTotal:    pages,
			PagesByStatus: map[string]int{string(models.PageStatusProofread): 1, string(models.PageStatusMachineProofread): 2},
		}
	}
	sub := book.Section{Title: "Диалектический метод", ChapterID: 11, Blocks: []book.Block{
		{Pages: []book.Page{page(3, 3, "Метод.")}},
	}}
	volume := &book.Book{Sections: []book.Section{
		{Title: "«Без заглавия»", Blocks: []book.Block{{Pages: []book.Page{page(1, 1, "Титул.")}}}},
		{Title: "Анархизм или социализм?", ChapterID: 10, Blocks: []book.Block{
			{Pages: []book.Page{page(2, 2, "Начало.")}},
			{Child: &sub},
		}},
		{Title: `Глава & <i>курсив</i>`, ChapterID: 12, Blocks: []book.Block{{Pages: []book.Page{page(4, 4, "Конец.")}}}},
		// Секция без полос — файла не будет.
		{Title: "Пустая", ChapterID: 13},
	}}
	front := &models.Work{ID: 103, Title: "И. В. Сталин. Сочинения. Том 1. Предваряющие материалы",
		Slug: "stalin-t01-front", Role: models.WorkRoleFrontMatter}
	return &fakeSource{
		shelf: &models.Shelf{Editions: []models.ShelfEdition{{
			Edition: ed,
			Volumes: []*models.VolumeSummary{vol(100, 1, 4), vol(101, 2, 0), vol(102, 3, 5)},
		}}},
		highlights: []models.EditionHighlight{{EditionID: 7, ChapterID: 11, ChapterTitle: "Диалектический метод"}},
		front:      map[int64][]*models.Work{100: {front}},
		volumes: map[int64]*book.Book{
			100: volume,
			103: {Sections: []book.Section{{Title: "«Без заглавия»", Blocks: []book.Block{{Pages: []book.Page{page(1, 1, "Содержание.")}}}}}},
		},
		concepts: map[string]*Concept{},
	}
}

// build собирает фикстуру во временный каталог и отдаёт путь к нему.
func build(t *testing.T, src *fakeSource) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	g := &Generator{Src: src, Out: out, BaseURL: "https://lib.example.org", BuildDate: "2026-10-06"}
	if err := g.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out
}

func read(t *testing.T, out, p string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
	if err != nil {
		t.Fatalf("нет файла %s: %v", p, err)
	}
	return string(data)
}

func exists(out, p string) bool {
	_, err := os.Stat(filepath.Join(out, filepath.FromSlash(p)))
	return err == nil
}

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: нет %q", what, w)
		}
	}
}
