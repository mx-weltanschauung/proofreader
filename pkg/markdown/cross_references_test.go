package markdown

import (
	"strings"
	"testing"
)

func TestCrossReferencesInTextAndNotes(t *testing.T) {
	r := NewRenderer()
	var found []BibliographicReference
	p := PageContent{PageNumber: 8, Content: "См. т. 2, с. 47; уже [т. 2, с. 48](/where); `т. 2, с. 49`; том 2, стр. 50—52; том 2, стр. 53, 54; см. примечание 8.[^s1]\n\n[^s1]: См. Сочинения, 5 изд., том 3, стр. 13.\n"}
	p.Decorate = func(note, html string) string {
		return LinkBibliographicReferences(html, func(ref BibliographicReference) int64 {
			found = append(found, ref)
			if note != "" && ref.EditionNumber != 5 {
				t.Errorf("edition in note = %d", ref.EditionNumber)
			}
			if ref.PrintedPage == 13 {
				return 7
			}
			return 0
		})
	}
	html, notes := r.CollectPages([]PageContent{p})
	if len(found) != 2 || found[0].Volume != 2 || found[0].PrintedPage != 47 || found[1].Volume != 3 {
		t.Fatalf("unexpected references: %+v", found)
	}
	if strings.Contains(html[0], "bibliographic-reference") || !strings.Contains(RenderNotes(notes), `/api/cross-references/7/open`) {
		t.Fatalf("only the chosen note should link: %s %s", html[0], RenderNotes(notes))
	}
	if !strings.Contains(r.RenderPageWithNotes("", p), `/api/cross-references/7/open`) {
		t.Fatal("link missing in page view")
	}
}

func TestCrossReferenceContextAcrossInlineMarkup(t *testing.T) {
	r := NewRenderer()
	content := r.Render("Сочинения, **4 изд.**, том 3, стр. 47. Другой адрес: том 3, стр. 49**—50**.\n\nСм. т. 2, с. 48.")
	var refs []BibliographicReference
	result := LinkBibliographicReferences(content, func(ref BibliographicReference) int64 {
		refs = append(refs, ref)
		return 29
	})
	if len(refs) != 2 || refs[0].EditionNumber != 4 || !strings.Contains(refs[0].Context, "Сочинения, 4 изд.") ||
		refs[1].Volume != 2 || refs[1].PrintedPage != 48 {
		t.Fatalf("inline markup cut the bibliographic context: %+v", refs)
	}
	if !strings.Contains(result, `<strong>4 изд.</strong>, <a class="bibliographic-reference"`) ||
		!strings.Contains(result, `том 3, стр. 49<strong>—50</strong>`) {
		t.Fatalf("rendering changed markup or linked the range: %s", result)
	}
}
