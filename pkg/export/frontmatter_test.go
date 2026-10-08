package export

import (
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
)

func ptrInt64(v int64) *int64 { return &v }

func TestNormalizeBody(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"crlf becomes lf", "первая\r\nвторая\r\n", "первая\nвторая\n"},
		{"missing trailing newline is added", "текст", "текст\n"},
		{"extra trailing newlines collapse to one", "текст\n\n\n", "текст\n"},
		{"empty stays empty", "", ""},
		{"only newlines become empty", "\n\n", ""},
		{"inner blank lines survive", "а\n\nб\n", "а\n\nб\n"},
		{"bare cr becomes lf", "первая\rвторая", "первая\nвторая\n"},
		{"bare cr at end collapses like lf", "текст\r\r", "текст\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeBody(c.in); got != c.want {
				t.Errorf("normalizeBody(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestQuotedEscapes(t *testing.T) {
	if got, want := quoted(`Том "1"`), `"Том \"1\""`; got != want {
		t.Errorf("quoted() = %s, want %s", got, want)
	}
	if got, want := quoted(`c:\путь`), `"c:\\путь"`; got != want {
		t.Errorf("quoted() = %s, want %s", got, want)
	}
}

// TestQuotedFlattensNewlines pins the finding: quoted() is used for
// renderWorkFile's frontmatter fields, unlike renderWorksIndex's table cells
// which already went through oneLine via cell(). A raw newline (or bare CR)
// in title/author would otherwise break the frontmatter line.
func TestQuotedFlattensNewlines(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"lf", "Том\n1", `"Том 1"`},
		{"crlf", "Том\r\n1", `"Том 1"`},
		{"bare cr", "Том\r1", `"Том 1"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoted(c.in); got != c.want {
				t.Errorf("quoted(%q) = %s, want %s", c.in, got, c.want)
			}
		})
	}
}

// TestOneLineFlattensBareCR covers the case oneLine's own doc comment did not
// mention before this fix: a lone \r (not part of a CRLF pair) must also
// collapse to a space, matching normalizeBody's handling of it.
func TestOneLineFlattensBareCR(t *testing.T) {
	if got, want := oneLine("а\rб"), "а б"; got != want {
		t.Errorf("oneLine(%q) = %q, want %q", "а\rб", got, want)
	}
}

func TestRenderWorkFile(t *testing.T) {
	date := time.Date(1954, 3, 1, 0, 0, 0, 0, time.UTC)
	work := &models.Work{
		ID: 1, Title: `Том: "первый"`, Author: "Автор",
		Language: "ru", Country: "ru",
		Status: models.WorkStatusInProgress, PublicationDate: &date,
	}

	want := "---\n" +
		"id: 1\n" +
		"title: \"Том: \\\"первый\\\"\"\n" +
		"author: \"Автор\"\n" +
		"language: \"ru\"\n" +
		"country: \"ru\"\n" +
		"status: in_progress\n" +
		"page_offset: 0\n" +
		"publication_date: 1954-03-01\n" +
		"---\n"

	if got := renderWorkFile(work); got != want {
		t.Errorf("renderWorkFile() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderWorkFileWithoutDate(t *testing.T) {
	work := &models.Work{ID: 2, Title: "Т", Author: "А", Language: "ru", Country: "ru", Status: models.WorkStatusDraft}

	got := renderWorkFile(work)
	if strings.Contains(got, "publication_date") {
		t.Errorf("nil publication_date must be absent, got:\n%s", got)
	}
	if strings.Contains(got, "null") {
		t.Errorf("nil values must never render as null, got:\n%s", got)
	}
}

func TestRenderPageFile(t *testing.T) {
	page := &models.Page{
		PageNumber: 7, Status: models.PageStatusProofread,
		ChapterID: ptrInt64(12), ContentMarkdown: "Текст\r\nстраницы",
	}

	want := "---\npage_number: 7\nstatus: вычитана\n---\nТекст\nстраницы\n"

	if got := renderPageFile(page); got != want {
		t.Errorf("renderPageFile() =\n%q\nwant\n%q", got, want)
	}
}

// TestRenderPageFileChapterIDIgnored pins down that ChapterID never reaches
// the frontmatter, even when set: recreating chapters (toc-chapters skill)
// reassigns every chapter's id without touching page text, and a chapter_id
// field would turn that into a diff across every page file in the work.
func TestRenderPageFileChapterIDIgnored(t *testing.T) {
	withChapter := &models.Page{PageNumber: 7, Status: models.PageStatusProofread, ChapterID: ptrInt64(12)}
	withoutChapter := &models.Page{PageNumber: 7, Status: models.PageStatusProofread}

	got := renderPageFile(withChapter)
	if strings.Contains(got, "chapter_id") {
		t.Errorf("chapter_id must never appear in page frontmatter, got:\n%s", got)
	}
	if got != renderPageFile(withoutChapter) {
		t.Errorf("ChapterID must not affect renderPageFile output: %q vs %q", got, renderPageFile(withoutChapter))
	}
}

func TestRenderPageFileWithoutChapter(t *testing.T) {
	page := &models.Page{PageNumber: 1, Status: models.PageStatusEmpty}

	want := "---\npage_number: 1\nstatus: пустая_страница\n---\n"

	if got := renderPageFile(page); got != want {
		t.Errorf("renderPageFile() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderWorkFileVolumeFields(t *testing.T) {
	part := "II"
	num := 25
	var edID int64 = 3
	w := &models.Work{
		ID: 7, Title: "Сочинения. Том 25, часть II", Status: models.WorkStatusDraft,
		EditionID: &edID, VolumeNumber: &num, VolumePart: &part, PageOffset: -8,
		ShelfLabel:  "Материализм и эмпириокритицизм",
		Description: "В книге недостаёт нескольких страниц",
	}
	got := renderWorkFile(w)
	for _, want := range []string{
		"edition_id: 3", "volume_number: 25", `volume_part: "II"`, "page_offset: -8",
		`shelf_label: "Материализм и эмпириокритицизм"`,
		`description: "В книге недостаёт нескольких страниц"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("frontmatter не содержит %q:\n%s", want, got)
		}
	}

	// У работы без тома координаты выпадают из frontmatter целиком
	// (renderFrontmatter выбрасывает пустые поля), а page_offset — поле не
	// указательное, а обычное числовое: 0 должен остаться в выгрузке, иначе
	// импорт не отличит «смещения нет» от «поле потерялось». shelf_label —
	// тоже обычная строка (не указатель), но пустая означает «подписи нет»,
	// так что, в отличие от page_offset, при пустом значении строка должна
	// выпасть из frontmatter, а не остаться как shelf_label: "".
	bare := &models.Work{ID: 8, Title: "Указатель", Status: models.WorkStatusDraft}
	got = renderWorkFile(bare)
	for _, unwanted := range []string{"edition_id", "volume_number", "volume_part", "shelf_label", "description"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("у работы без тома не должно быть строки %q:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "page_offset: 0\n") {
		t.Errorf("у работы без тома ожидался page_offset: 0:\n%s", got)
	}
}

func TestRenderWorksIndex(t *testing.T) {
	works := []*models.Work{
		{ID: 1, Title: "Том | 1", Author: "Автор", Status: models.WorkStatusInProgress},
		{ID: 2, Title: "Том\n2", Author: "", Status: models.WorkStatusCompleted},
	}

	want := "| id | Название | Автор | Статус |\n" +
		"|---|---|---|---|\n" +
		"| 1 | Том \\| 1 | Автор | in_progress |\n" +
		"| 2 | Том 2 |  | completed |\n"

	if got := renderWorksIndex(works); got != want {
		t.Errorf("renderWorksIndex() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderChaptersFileNestsAndOrders(t *testing.T) {
	child2 := &models.Chapter{ID: 14, Title: "Глава 2", Type: "chapter", OrderNumber: 2, StartPage: 49, EndPage: 90}
	child1 := &models.Chapter{ID: 13, Title: "Глава 1", Type: "chapter", OrderNumber: 1, StartPage: 12, EndPage: 48}
	root := &models.Chapter{
		ID: 12, Title: "Предисловие", Type: "part", OrderNumber: 1, StartPage: 5, EndPage: 11,
		Children: []*models.Chapter{child2, child1}, // нарочно в обратном порядке
	}

	want := "- [12] Предисловие — part, 5–11\n" +
		"  - [13] Глава 1 — chapter, 12–48\n" +
		"  - [14] Глава 2 — chapter, 49–90\n"

	if got := renderChaptersFile([]*models.Chapter{root}); got != want {
		t.Errorf("renderChaptersFile() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderChaptersFileEmpty(t *testing.T) {
	if got := renderChaptersFile(nil); got != "" {
		t.Errorf("renderChaptersFile(nil) = %q, want empty", got)
	}
}

// parent_work_id в выгрузку не идёт: идентификаторы работ между
// восстановлениями не стабильны (у тома 5 он менялся 10 → 41), и записанное
// в git число протухло бы молча.
func TestRenderWorkFileRoleAndNumbering(t *testing.T) {
	parent := int64(41)
	w := &models.Work{
		ID: 42, Title: "Том 5. Предваряющие материалы", Language: "ru",
		Status: models.WorkStatusDraft, PageOffset: -1,
		Role: models.WorkRoleFrontMatter, NumberingStyle: models.NumberingRoman,
		ParentWorkID: &parent,
	}
	got := renderWorkFile(w)
	for _, want := range []string{`role: front_matter`, `numbering_style: roman`, `page_offset: -1`} {
		if !strings.Contains(got, want) {
			t.Errorf("выгрузка не содержит %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "parent_work_id") {
		t.Errorf("parent_work_id не должен попадать в выгрузку:\n%s", got)
	}
}
