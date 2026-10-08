package markdown

import (
	"regexp"
	"strings"
	"testing"
)

// Полоса 53 одиннадцатого тома Ленина в сокращении: одно примечание тома (27)
// и шесть подстрочных. Голый Render оставлял под текстом <ol> gomarkdown, и
// браузер нумеровал его подряд — 27 читалось «1», (1) — «2» и так до «7».
const leninPage53 = "Парламента еще у нас нет, а парламентского кретинизма[^27] сколько угодно.\n\n" +
	"Вместе с оглашением[^s1] такого порядка выборов должна быть узаконена[^s2] свобода, " +
	"и на ее членов[^s3]. Если комиссия откажется[^s4], потребуем собрания[^s5], " +
	"и оба вместе[^s6] пойдут к республике.\n\n" +
	"[^s1]: В «Искре»?\n\n" +
	"[^s2]: Николаем?\n\n" +
	"[^s3]: Вот что значит тактика!\n\n" +
	"[^s4]: Не может этого быть!\n\n" +
	"[^s5]: В рукописи далее зачеркнуто. *Ред.*\n\n" +
	"[^s6]: И вооруженный пролетариат?\n\n" +
	"[^27]: Выражение *«парламентский кретинизм»* употреблялось Марксом. — *53.*\n"

// fnItemRe — один пункт блока сносок в форме RenderNotes: id пункта, вид и
// маркер, который видит читатель.
var fnItemRe = regexp.MustCompile(
	`<li id="fn:([^"]+)" class="fn-item fn-item--(\w+)"><a class="fn-back" href="#fnref:([^"]+)">([^<]*)</a>`)

type renderedNote struct{ name, kind, backRef, marker string }

func notesOf(html string) []renderedNote {
	var out []renderedNote
	for _, m := range fnItemRe.FindAllStringSubmatch(html, -1) {
		out = append(out, renderedNote{m[1], m[2], m[3], m[4]})
	}
	return out
}

func TestRenderWithNotesPrintsRealMarkers(t *testing.T) {
	html := NewRenderer().RenderWithNotes("", 53, leninPage53)

	if strings.Contains(html, "<ol") {
		t.Fatalf("блок сносок остался нумерованным списком gomarkdown: %s", html)
	}
	got := notesOf(html)
	want := []renderedNote{
		{"53-s1", "subscript", "53-s1", "(1)"},
		{"53-s2", "subscript", "53-s2", "(2)"},
		{"53-s3", "subscript", "53-s3", "(3)"},
		{"53-s4", "subscript", "53-s4", "(4)"},
		{"53-s5", "subscript", "53-s5", "(5)"},
		{"53-s6", "subscript", "53-s6", "(6)"},
		{"53-27", "endnote", "53-27", "27"},
	}
	if len(got) != len(want) {
		t.Fatalf("пунктов %d, ждали %d: %+v\n%s", len(got), len(want), got, html)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("пункт %d: %+v, ждали %+v", i, got[i], want[i])
		}
	}
	for _, caption := range []string{"Подстрочные примечания", "Примечания"} {
		if !strings.Contains(html, caption) {
			t.Errorf("нет подписи раздела %q", caption)
		}
	}
	// Под вырезкой и на полосе блок стоит внутри текста — заголовком он
	// встал бы узлом оглавления, как под вклейкой разбора.
	if strings.Contains(html, "<h2") {
		t.Errorf("подпись раздела вышла заголовком: %s", html)
	}
	// Маркер в тексте и пункт списка ведут друг на друга.
	for _, n := range want {
		sup := `<sup class="footnote-ref footnote-ref--` + n.kind + `" id="fnref:` + n.name +
			`"><a href="#fn:` + n.name + `">` + n.marker + `</a></sup>`
		if !strings.Contains(html, sup) {
			t.Errorf("в тексте нет маркера %s: ждали %s", n.marker, sup)
		}
	}
}

// Вырезка, начатая с середины полосы, несёт номера своей полосы: (3), а не
// (1) — иначе номер разойдётся со страницей тома, куда ведёт вырезка.
func TestRenderWithNotesKeepsPageNumbering(t *testing.T) {
	src := "Если комиссия откажется[^s4], потребуем собрания[^s5].\n\n" +
		"[^s4]: Не может этого быть!\n\n[^s5]: Зачеркнуто.\n"
	got := notesOf(NewRenderer().RenderWithNotes("", 53, src))
	if len(got) != 2 || got[0].marker != "(4)" || got[1].marker != "(5)" {
		t.Fatalf("маркеры перенумерованы: %+v", got)
	}
}

// Примечания тома идут в книжном порядке, а не в порядке ссылок.
func TestRenderWithNotesSortsEndnotesByBookNumber(t *testing.T) {
	src := "Раз[^31], два[^27].\n\n[^31]: тридцать первое\n\n[^27]: двадцать седьмое\n"
	got := notesOf(NewRenderer().RenderWithNotes("", 53, src))
	if len(got) != 2 || got[0].marker != "27" || got[1].marker != "31" {
		t.Fatalf("порядок примечаний тома: %+v", got)
	}
}

// Две записи потока понятия с одной и той же полосы — один DOM. Область имён
// разводит их якоря, и превью сноски (document.getElementById) не берёт тело
// чужой записи.
func TestRenderWithNotesScopeSeparatesAnchors(t *testing.T) {
	r := NewRenderer()
	src := "Текст[^s1].\n\n[^s1]: примечание\n"
	first := notesOf(r.RenderWithNotes("c1-", 53, src))
	second := notesOf(r.RenderWithNotes("c2-", 53, src))
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("пункты: %+v / %+v", first, second)
	}
	if first[0].name != "c1-53-s1" || second[0].name != "c2-53-s1" {
		t.Fatalf("области не развели якоря: %q / %q", first[0].name, second[0].name)
	}
	if first[0].marker != "(1)" {
		t.Fatalf("область испортила маркер: %q", first[0].marker)
	}
}

// Текст без сносок выходит тем же, что у Render: блока сносок нет вовсе.
func TestRenderWithNotesWithoutNotesMatchesRender(t *testing.T) {
	r := NewRenderer()
	src := "# Заголовок\n\nАбзац с *курсивом* и формулой.\n"
	if got, want := r.RenderWithNotes("c7-", 53, src), r.Render(src); got != want {
		t.Fatalf("без сносок вывод разошёлся:\n%s\n---\n%s", got, want)
	}
}
