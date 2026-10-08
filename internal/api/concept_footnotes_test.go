// Сноски в вырезках понятия: настоящие номера под текстом и якоря, не
// сталкивающиеся между записями одной страницы понятия.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"proofreader/internal/models"
)

// Полоса с примечанием тома и подстрочным — форма полосы 53 одиннадцатого
// тома Ленина, где блок сносок вырезки нумеровался браузером подряд.
const pageWithBothNotes = "Парламентского кретинизма[^27] сколько угодно, и оглашение[^s1] порядка.\n\n" +
	"[^s1]: В «Искре»?\n\n" +
	"[^27]: Выражение употреблялось Марксом.\n"

var noteItemIDRe = regexp.MustCompile(`<li id="(fn:[^"]+)" class="fn-item`)

// Обе записи conceptWithTwoRubrics стоят на одних и тех же полосах 730—731 и
// попадают в один DOM. С одинаковыми id превью сноски второй записи
// (document.getElementById) брало тело первой.
func TestFragmentsSameNoteInTwoEntriesGetsDistinctAnchors(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: pageWithBothNotes},
		{ID: 9144, WorkID: 14, PageNumber: 731, ContentMarkdown: "продолжение"},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	rec, body := fragmentsRequest(t, h, "")
	if rec.Code != http.StatusOK || len(body.Entries) != 2 {
		t.Fatalf("код %d, записей %d", rec.Code, len(body.Entries))
	}

	seen := map[string]int64{}
	for _, e := range body.Entries {
		html := e.Cuts[0].Parts[0].HTML
		if strings.Contains(html, "<ol") {
			t.Fatalf("запись %d: блок сносок нумерует браузер: %s", e.ReferenceID, html)
		}
		for _, want := range []string{
			fmt.Sprintf(`id="fn:c%d-730-s1"`, e.ReferenceID),
			fmt.Sprintf(`id="fn:c%d-730-27"`, e.ReferenceID),
			`<a class="fn-back" href="#fnref:c` + fmt.Sprint(e.ReferenceID) + `-730-s1">(1)</a>`,
			`<a class="fn-back" href="#fnref:c` + fmt.Sprint(e.ReferenceID) + `-730-27">27</a>`,
		} {
			if !strings.Contains(html, want) {
				t.Errorf("запись %d: нет %s в %s", e.ReferenceID, want, html)
			}
		}
		for _, m := range noteItemIDRe.FindAllStringSubmatch(html, -1) {
			if other, dup := seen[m[1]]; dup {
				t.Errorf("id %s повторяется в записях %d и %d", m[1], other, e.ReferenceID)
			}
			seen[m[1]] = e.ReferenceID
		}
	}
}

// Ответ правки границ заменяет запись в потоке на месте (replaceEntry), и её
// якоря обязаны остаться теми же, что дал поток.
func TestPutCutsResponseUsesStreamAnchors(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: pageWithBothNotes},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, newFakeFragmentStore())

	rec := putCutsRequestFor(t, h, "541", fmt.Sprintf(
		`{"status":"machine","cuts":[{"start_page":730,"start_offset":0,"end_page":730,"end_offset":%d}]}`,
		strings.Index(pageWithBothNotes, "\n\n")))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
	}
	// Ответ — JSON, кавычки атрибутов в нём экранированы.
	if !strings.Contains(rec.Body.String(), `id=\"fn:c541-730-s1\"`) {
		t.Fatalf("якорь ответа правки разошёлся с потоком: %s", rec.Body.String())
	}
}

// Раскрытая полоса стоит в том же DOM, что и вырезка этой записи: у неё своя
// область, иначе её сноски совпали бы с id вырезки.
func TestExpandPageNotesHaveOwnAnchors(t *testing.T) {
	pages := &pageStub{pages: []*models.Page{
		{ID: 9143, WorkID: 14, PageNumber: 730, ContentMarkdown: pageWithBothNotes},
	}}
	h := fragmentsHandler(conceptWithTwoRubrics(), pages, nil, nil)

	rec, got := expandRequest(t, h, "473", "9143")
	if rec.Code != http.StatusOK || len(got.Chunks) != 1 {
		t.Fatalf("код %d, кусков %d", rec.Code, len(got.Chunks))
	}
	html := got.Chunks[0].HTML
	if strings.Contains(html, "<ol") {
		t.Fatalf("блок сносок нумерует браузер: %s", html)
	}
	for _, want := range []string{`id="fn:e473-730-s1"`, `id="fn:e473-730-27"`} {
		if !strings.Contains(html, want) {
			t.Errorf("нет %s в %s", want, html)
		}
	}
}

// Одиночная страница тома рендерилась тем же голым Render, что и вырезка, —
// и тем же <ol>, нумерующим примечание 27 единицей.
func TestPageRenderPrintsRealNoteMarkers(t *testing.T) {
	page, r := pageOfWorkTestRig(t)
	page.ContentMarkdown = pageWithBothNotes

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/works/1/pages/7/render", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d; тело %s", rec.Code, rec.Body.String())
	}
	var body struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if strings.Contains(body.HTML, "<ol") {
		t.Fatalf("блок сносок нумерует браузер: %s", body.HTML)
	}
	for _, want := range []string{
		`<a class="fn-back" href="#fnref:3-s1">(1)</a>`,
		`<a class="fn-back" href="#fnref:3-27">27</a>`,
	} {
		if !strings.Contains(body.HTML, want) {
			t.Errorf("нет %s в %s", want, body.HTML)
		}
	}
}
