package api

import (
	"testing"

	"proofreader/internal/models"
	"proofreader/pkg/markdown"
)

func TestBuildNoteIndex(t *testing.T) {
	pages := []*models.Page{
		{ID: 10, PageNumber: 106, ContentMarkdown: "текст\n\n[^51]: «Deutsche Jahrbücher» — журнал."},
		{ID: 20, PageNumber: 474, ContentMarkdown: "b\n\n[^132]: «Hallische Jahrbücher» — см. примечание 51."},
	}
	idx := buildNoteIndex(pages, markdown.NewRenderer())
	if len(idx) != 2 {
		t.Fatalf("want 2 entries, got %d", len(idx))
	}
	if idx[0].Number != 51 || idx[1].Number != 132 {
		t.Fatalf("wrong order/numbers: %+v", idx)
	}
	if idx[0].TargetPage != 106 || idx[0].TargetPageID != 10 {
		t.Errorf("wrong target for 51: %+v", idx[0])
	}
	// body rendered to HTML: CompletePage wrapper stripped, outer <p> stripped
	if contains(idx[0].BodyHTML, "<!DOCTYPE") || contains(idx[0].BodyHTML, "<body") || contains(idx[0].BodyHTML, "<html") {
		t.Errorf("body_html should not contain full-document wrapper: %q", idx[0].BodyHTML)
	}
	if want := "«Deutsche Jahrbücher» — журнал."; idx[0].BodyHTML != want {
		t.Errorf("body_html = %q, want %q", idx[0].BodyHTML, want)
	}
	// nested cross-ref inside a note body is itself wrapped
	if !contains(idx[1].BodyHTML, `data-note="51"`) {
		t.Errorf("nested xref not wrapped: %q", idx[1].BodyHTML)
	}
}

func TestBuildNoteIndex_Empty(t *testing.T) {
	if idx := buildNoteIndex(nil, markdown.NewRenderer()); len(idx) != 0 {
		t.Errorf("want empty, got %+v", idx)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
