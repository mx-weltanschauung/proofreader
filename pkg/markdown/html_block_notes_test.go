package markdown

import (
	"regexp"
	"strings"
	"testing"
)

// tableRef is the shape that occurs in the corpus: an OCR'd HTML table with a
// footnote reference inside a cell, and the definition after the table.
const tableRef = "<table><tbody><tr><td>товар в центнерах[^s1]</td><td>35 349</td></tr></tbody></table>\n\n" +
	"[^s1]: Английский центнер составляет ¹⁄₂₀ большой тонны. *Ред.*\n"

func TestHTMLBlockNoteRefRendersOnPage(t *testing.T) {
	out := NewRenderer().Render(tableRef)

	if strings.Contains(out, "[^s1]") {
		t.Errorf("маркер остался буквальным текстом:\n%s", out)
	}
	if !strings.Contains(out, "Английский центнер") {
		t.Errorf("тело сноски потеряно:\n%s", out)
	}
	// маркер должен стоять в той же ячейке, где он был в источнике
	if !regexp.MustCompile(`центнерах<sup class="footnote-ref[^"]*" id="fnref:s1">`).MatchString(out) {
		t.Errorf("sup не встал в ячейку таблицы:\n%s", out)
	}
	if !strings.Contains(out, `<a href="#fn:s1">(1)</a>`) {
		t.Errorf("подстрочный маркер должен быть «(1)»:\n%s", out)
	}
	if !strings.Contains(out, "footnote-ref--subscript") {
		t.Errorf("не проставлен класс вида сноски:\n%s", out)
	}
	if !strings.Contains(out, "<table>") {
		t.Errorf("таблица должна остаться таблицей:\n%s", out)
	}
}

func TestHTMLBlockNoteRefLeavesNoScaffolding(t *testing.T) {
	out := NewRenderer().Render(tableRef)

	if strings.Contains(out, carrierSentinel) {
		t.Errorf("служебный носитель ссылки просочился в выдачу:\n%s", out)
	}
	// ровно один sup на одну ссылку — носитель не должен добавить второй
	if n := len(regexp.MustCompile(`<sup`).FindAllString(out, -1)); n != 1 {
		t.Errorf("ожидался 1 sup, найдено %d:\n%s", n, out)
	}
	// <p><table> — невалидная вложенность; таблица не должна оказаться в абзаце
	if strings.Contains(out, "<p><table>") {
		t.Errorf("таблица завёрнута в абзац:\n%s", out)
	}
}

func TestHTMLBlockNoteRefBackLinkTargetsTheCell(t *testing.T) {
	out := NewRenderer().Render(tableRef)

	// возврат из тела сноски должен вести на тот sup, что стоит в ячейке
	if !strings.Contains(out, `href="#fnref:s1"`) {
		t.Errorf("нет обратной ссылки на маркер в ячейке:\n%s", out)
	}
}

func TestHTMLBlockEndnoteRefKeepsBookNumber(t *testing.T) {
	// работа 15 стр. 196 — в таблице стоит номерной эндноут
	md := "<table><tbody><tr><td>вывоз[^83]</td></tr></tbody></table>\n\n[^83]: Имеется в виду отчёт.\n"
	out := NewRenderer().Render(md)

	if !strings.Contains(out, `<a href="#fn:83">83</a>`) {
		t.Errorf("эндноут должен сохранить книжный номер 83:\n%s", out)
	}
	if !strings.Contains(out, "footnote-ref--endnote") {
		t.Errorf("не проставлен класс endnote:\n%s", out)
	}
	if !strings.Contains(out, "Имеется в виду отчёт") {
		t.Errorf("тело эндноута потеряно:\n%s", out)
	}
}

func TestSeveralNoteRefsInOneTable(t *testing.T) {
	// работа 14 стр. 380 — две редакционные сноски в одной таблице
	md := "<table><tbody><tr><td>а[^r1]</td><td>б[^r2]</td></tr></tbody></table>\n\n" +
		"[^r1]: Первое. *Ред.*\n\n[^r2]: Второе. *Ред.*\n"
	out := NewRenderer().Render(md)

	for _, want := range []string{`id="fnref:r1"`, `id="fnref:r2"`, "Первое", "Второе"} {
		if !strings.Contains(out, want) {
			t.Errorf("нет %q в выдаче:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[^r") {
		t.Errorf("остался буквальный маркер:\n%s", out)
	}
}

func TestNoteRefInTableGetsReadingOrderMarkerInChapter(t *testing.T) {
	// В главе подстрочные нумеруются по порядку появления в тексте. Ссылка из
	// таблицы стоит РАНЬШЕ обычной, значит ей и достаётся «(1)».
	content := "<table><tbody><tr><td>из таблицы[^s2]</td></tr></tbody></table>\n\n" +
		"обычная ссылка[^s1] в абзаце\n\n" +
		"[^s1]: Тело обычной. *Ред.*\n\n[^s2]: Тело табличной. *Ред.*\n"

	pageHTML, notes := NewRenderer().CollectPages([]PageContent{{PageNumber: 500, Content: content}})
	joined := strings.Join(pageHTML, "\n")

	if strings.Contains(joined, "[^500-s2]") || strings.Contains(joined, "[^s2]") {
		t.Errorf("внутреннее имя сноски утекло в текст главы:\n%s", joined)
	}
	if len(notes.Subscript) != 2 {
		t.Fatalf("ожидались 2 подстрочные заметки, получено %d", len(notes.Subscript))
	}
	byBody := map[string]string{}
	for _, n := range notes.Subscript {
		switch {
		case strings.Contains(n.BodyHTML, "табличной"):
			byBody["таблица"] = n.Marker
		case strings.Contains(n.BodyHTML, "обычной"):
			byBody["абзац"] = n.Marker
		}
	}
	if byBody["таблица"] != "(1)" || byBody["абзац"] != "(2)" {
		t.Errorf("маркеры не по порядку чтения: таблица=%q абзац=%q",
			byBody["таблица"], byBody["абзац"])
	}
}

func TestNoteRefInFencedCodeIsLeftAlone(t *testing.T) {
	md := "```\n<table><tbody><tr><td>пример[^s1]</td></tr></tbody></table>\n```\n\n[^s1]: Тело. *Ред.*\n"
	out := NewRenderer().Render(md)

	if !strings.Contains(out, "[^s1]") {
		t.Errorf("ссылка внутри блока кода должна остаться текстом:\n%s", out)
	}
	if strings.Contains(out, "<sup") {
		t.Errorf("в блоке кода не должно появиться sup:\n%s", out)
	}
}

func TestPlainNoteRefsUnaffected(t *testing.T) {
	// регресс: обычная ссылка в абзаце обрабатывается как раньше
	out := NewRenderer().Render("текст[^s1] дальше\n\n[^s1]: Тело. *Ред.*\n")

	if !strings.Contains(out, `<sup class="footnote-ref footnote-ref--subscript" id="fnref:s1">`) {
		t.Errorf("обычная ссылка сломана:\n%s", out)
	}
	if n := len(regexp.MustCompile(`<sup`).FindAllString(out, -1)); n != 1 {
		t.Errorf("ожидался 1 sup, найдено %d:\n%s", n, out)
	}
}

func TestHTMLTableWithoutNoteRefsUntouched(t *testing.T) {
	md := "<table><tbody><tr><td>без сносок</td></tr></tbody></table>\n\nабзац\n"
	out := NewRenderer().Render(md)

	if strings.Contains(out, carrierSentinel) {
		t.Errorf("носитель добавлен там, где сносок нет:\n%s", out)
	}
	if !strings.Contains(out, "<table><tbody><tr><td>без сносок</td></tr></tbody></table>") {
		t.Errorf("таблица без сносок изменилась:\n%s", out)
	}
}

func TestHoistLeavesMarkdownTableAlone(t *testing.T) {
	// markdown-таблицу gomarkdown разбирает сам, вмешиваться не нужно
	md := "| колонка |\n| --- |\n| ячейка[^s1] |\n\n[^s1]: Тело. *Ред.*\n"
	out := NewRenderer().Render(md)

	if strings.Contains(out, carrierSentinel) {
		t.Errorf("носитель добавлен в markdown-таблицу:\n%s", out)
	}
	if !strings.Contains(out, `id="fnref:s1"`) || !strings.Contains(out, "Тело") {
		t.Errorf("сноска в markdown-таблице сломана:\n%s", out)
	}
	if n := len(regexp.MustCompile(`<sup`).FindAllString(out, -1)); n != 1 {
		t.Errorf("ожидался 1 sup, найдено %d:\n%s", n, out)
	}
}

func TestHoistHandlesMultiLineTable(t *testing.T) {
	// работа 14 стр. 380 — таблица разбита на строки, пустых строк внутри нет
	md := "<table>\n<tbody>\n<tr><td>ячейка[^s1]</td></tr>\n</tbody>\n</table>\n\n[^s1]: Тело. *Ред.*\n"
	out := NewRenderer().Render(md)

	if strings.Contains(out, "[^s1]") {
		t.Errorf("маркер в многострочной таблице не обработан:\n%s", out)
	}
	if !strings.Contains(out, "Тело") {
		t.Errorf("тело сноски потеряно:\n%s", out)
	}
}
