package export

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"proofreader/internal/models"
)

// fakeSource is an in-memory Source. Any field left nil returns zero values;
// the *Err fields make a stage fail on demand. There is no worksErr: a
// ListWorks failure is equivalently covered by
// TestExportStoreErrorLeavesArchiveInvalid in internal/api, which exercises
// the same WriteZip code path through the real handler.
type fakeSource struct {
	works       []*models.Work
	chapters    map[int64][]*models.Chapter
	pages       map[int64][]*models.Page
	chaptersErr error
	pagesErr    error
}

func (f *fakeSource) ListWorks(ctx context.Context) ([]*models.Work, error) {
	return f.works, nil
}

func (f *fakeSource) ListChapters(ctx context.Context, workID int64) ([]*models.Chapter, error) {
	return f.chapters[workID], f.chaptersErr
}

func (f *fakeSource) ListPages(ctx context.Context, workID int64) ([]*models.Page, error) {
	return f.pages[workID], f.pagesErr
}

func twoWorkSource() *fakeSource {
	return &fakeSource{
		works: []*models.Work{
			{ID: 2, Title: "Том 2", Author: "А", Language: "ru", Country: "ru", Status: models.WorkStatusDraft},
			{ID: 1, Title: "Том 1", Author: "А", Language: "ru", Country: "ru", Status: models.WorkStatusInProgress},
		},
		chapters: map[int64][]*models.Chapter{
			1: {{ID: 12, Title: "Предисловие", Type: "part", OrderNumber: 1, StartPage: 1, EndPage: 3}},
		},
		pages: map[int64][]*models.Page{
			1: {
				{ID: 3, PageNumber: 3, Status: models.PageStatusNotProofread, ContentMarkdown: "три"},
				{ID: 1, PageNumber: 1, Status: models.PageStatusProofread, ContentMarkdown: "один"},
			},
			2: {{ID: 9, PageNumber: 1, Status: models.PageStatusEmpty}},
		},
	}
}

// entryNames returns archive entries in the order they were written.
func entryNames(t *testing.T, raw []byte) []string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("archive is not readable: %v", err)
	}
	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	return names
}

func TestWriteZipEntryOrder(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteZip(context.Background(), &buf, twoWorkSource()); err != nil {
		t.Fatalf("WriteZip() error = %v", err)
	}

	want := []string{
		"works.md",
		"works/0001/work.md",
		"works/0001/chapters.md",
		"works/0001/pages/0001.md",
		"works/0001/pages/0003.md",
		"works/0002/work.md",
		"works/0002/chapters.md",
		"works/0002/pages/0001.md",
	}

	got := entryNames(t, buf.Bytes())
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWriteZipIsDeterministic(t *testing.T) {
	var first, second bytes.Buffer
	if err := WriteZip(context.Background(), &first, twoWorkSource()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := WriteZip(context.Background(), &second, twoWorkSource()); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("two runs over the same data must produce byte-identical archives")
	}
}

func TestWriteZipPageContent(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteZip(context.Background(), &buf, twoWorkSource()); err != nil {
		t.Fatalf("WriteZip() error = %v", err)
	}

	raw := buf.Bytes()
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("archive is not readable: %v", err)
	}

	f, err := r.Open("works/0001/pages/0001.md")
	if err != nil {
		t.Fatalf("open page entry: %v", err)
	}
	defer f.Close()

	var content bytes.Buffer
	if _, err := content.ReadFrom(f); err != nil {
		t.Fatalf("read page entry: %v", err)
	}

	want := "---\npage_number: 1\nstatus: вычитана\n---\nодин\n"
	if content.String() != want {
		t.Errorf("page entry = %q, want %q", content.String(), want)
	}
}

func TestWriteZipEmptySource(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteZip(context.Background(), &buf, &fakeSource{}); err != nil {
		t.Fatalf("WriteZip() on empty source error = %v", err)
	}

	got := entryNames(t, buf.Bytes())
	if len(got) != 1 || got[0] != "works.md" {
		t.Errorf("entries = %v, want just works.md", got)
	}
}

func TestWriteZipSourceErrorLeavesArchiveInvalid(t *testing.T) {
	src := twoWorkSource()
	src.pagesErr = errors.New("database is gone")

	var buf bytes.Buffer
	err := WriteZip(context.Background(), &buf, src)
	if err == nil {
		t.Fatal("WriteZip() must return the source error")
	}

	raw := buf.Bytes()
	if _, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Error("a failed export must not produce a readable archive — the central directory must stay unwritten")
	}
}

// TestWriteZipChaptersErrorLeavesArchiveInvalid covers writeWork's early
// return on a ListChapters failure — the same shape of bug as pagesErr above,
// but on the chapters stage, which nothing else exercises.
func TestWriteZipChaptersErrorLeavesArchiveInvalid(t *testing.T) {
	src := twoWorkSource()
	src.chaptersErr = errors.New("database is gone")

	var buf bytes.Buffer
	err := WriteZip(context.Background(), &buf, src)
	if err == nil {
		t.Fatal("WriteZip() must return the source error")
	}

	raw := buf.Bytes()
	if _, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Error("a failed export must not produce a readable archive — the central directory must stay unwritten")
	}
}
