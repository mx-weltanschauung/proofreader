package book

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPDFWriterOneSheetPerSourcePageAndStableBytes(t *testing.T) {
	b := &Book{Meta: Meta{Title: "Проверочный том", Authors: []string{"Автор"}, URL: "https://example.org/works/1", Modified: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Proofread: Proofread{Total: 2, Human: 1, Machine: 1}}, Sections: []Section{{Title: "Глава", Blocks: []Block{{Pages: []Page{{Internal: 1, Printed: 7, Markdown: "Первый текст[^1].\n\n[^1]: Примечание."}, {Internal: 2, Printed: 8, Markdown: "Второй текст."}}}}}}}
	var first, second bytes.Buffer
	writer := PDFWriter{}
	if err := writer.Write(&first, b); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(&second, b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("PDF differs between identical writes")
	}
	if !bytes.HasPrefix(first.Bytes(), []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
	if n := len(regexp.MustCompile(`/Type /Page\b`).FindAll(first.Bytes(), -1)); n < 4 {
		t.Fatalf("only %d pages: want title, TOC, two source pages", n)
	}
	if writer.ContentType() != "application/pdf" || writer.Ext() != "pdf" {
		t.Fatal("incorrect format metadata")
	}
}

func TestPDFPlanKeepsDuplicatePrintedNumbersDistinct(t *testing.T) {
	b := &Book{Sections: []Section{
		{Title: "Одинаковое название", Blocks: []Block{{Pages: []Page{{Internal: 1, Printed: 5, Markdown: "Один."}}}}},
		{Title: "Одинаковое название", Blocks: []Block{{Pages: []Page{{Internal: 1, Printed: 5, Markdown: "Два."}}}}},
	}}
	plan := planPDF(b)
	if len(plan.pages) != 2 || len(plan.toc) != 2 {
		t.Fatalf("plan: %#v", plan)
	}
	if plan.toc[0].PageIndex == plan.toc[1].PageIndex {
		t.Fatal("duplicate numbers collapse TOC links")
	}
	if plan.pages[0].Printed != plan.pages[1].Printed {
		t.Fatal("fixture did not duplicate printed numbers")
	}
	if plan.pages[0].Source == plan.pages[1].Source {
		t.Fatal("collection sources are not disambiguated")
	}
}

func TestPDFTablePreservesCells(t *testing.T) {
	blocks, err := blocksFrom(`<table><tr><th>Год</th><th>Значение</th></tr><tr><td>1868</td><td>2 405</td></tr></table>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[0].Cols != 2 || blocks[1].Cells[1].Text != "2 405" {
		t.Fatalf("table = %#v", blocks)
	}
}

func TestPDFTitleNamesCollectionSources(t *testing.T) {
	b := &Book{Meta: Meta{Title: "Подборка"}, Sections: []Section{{Title: "Первая работа"}, {Title: "Вторая работа"}}}
	if got := pdfTitleSource(b); !strings.Contains(got, "2") || !strings.Contains(got, "содержании") {
		t.Fatalf("collection source line = %q", got)
	}
}

func TestPDFPlanUsesOriginalMarkdownBeforeExportNotePrefix(t *testing.T) {
	b := &Book{Sections: []Section{{Title: "Глава", Blocks: []Block{{Pages: []Page{{
		Printed:     5,
		Markdown:    "Текст[^s0-r1].\n\n[^s0-r1]: Подстрочное примечание.",
		RawMarkdown: "Текст[^r1].\n\n[^r1]: Подстрочное примечание.",
	}}}}}}}
	plan := planPDF(b)
	if got := plan.pages[0].Markdown; strings.Contains(got, "s0-r1") || !strings.Contains(got, "[^r1]") {
		t.Fatalf("PDF uses rewritten footnote names: %q", got)
	}
}

func TestPDFTableFootnoteLinksBothWays(t *testing.T) {
	b := &Book{Meta: Meta{Title: "Таблица", PageFrom: 1, PageTo: 1}, Sections: []Section{{Title: "Глава", Blocks: []Block{{Pages: []Page{{
		Internal: 1, Printed: 1,
		Markdown: "<table><tr><td>Текст[^s1]</td></tr></table>\n\n[^s1]: Подстрочное примечание.",
	}}}}}}}
	var out bytes.Buffer
	if err := (PDFWriter{}).Write(&out, b); err != nil {
		t.Fatal(err)
	}
	// Две области оглавления, знак в ячейке и обратная ссылка примечания.
	if n := bytes.Count(out.Bytes(), []byte("/Subtype /Link")); n < 4 {
		t.Fatalf("only %d links: table note cannot be opened and returned from", n)
	}
}

func TestPDFMathKeepsUnknownCommandText(t *testing.T) {
	for _, s := range []string{`\\frac{a}{b}`, `\\begin{cases}x&y\\\\ z&w\\end{cases}`, `\\text{капитал}`} {
		got := readableMath(s)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("lost formula %q", s)
		}
	}
}
