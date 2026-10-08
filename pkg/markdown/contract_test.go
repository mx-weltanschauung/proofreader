package markdown

import (
	"strings"
	"testing"
)

// contractMarkdown — разметка, в которой есть всё, чем рендер мог бы себя
// выдать: заголовок (гонит <h1>), подстрочная сноска с телом, несущим сырой
// <br> (его флаг UseXHTML не чинит — тело сноски идёт только в RenderNotes,
// и без него эта строка сторожа ничего не проверяет), и тот же сырой <br> в
// ручном HTML основного текста.
const contractMarkdown = "# Заголовок\n\nТекст[^1] и ещё текст.\n\n" +
	"<table><tr><td>ячейка<br>вторая строка</td></tr></table>\n\n" +
	"[^1]: Тело сноски.<br>вторая строка тела.\n"

// documentMarkers — признаки утечки нормализации наружу. Первые четыре —
// целый документ (обёртка html.CompletePage). Пятый — сырой незакрытый
// <br>: RenderNotes собирает <div class="footnotes"> вручную и не порождает
// ни одного из первых четырёх никогда, так что без этого маркера строка
// RenderNotes в TestPublicOutputsCarryNoDocumentWrapper не могла упасть ни
// при каком дефекте. В XHTML-выводе <br> закрыт (<br/>), поэтому подстрока
// "<br>" в нормализованном тексте не встречается — маркер срабатывает только
// на неснятой нормализации.
var documentMarkers = []string{"<!DOCTYPE", "<html", "<head", "GENERATOR", "<br>"}

// TestPublicOutputsCarryNoDocumentWrapper — сторож контракта пакета: наружу
// уходят только XHTML-фрагменты.
//
// Тест перечисляет публичные выходы поимённо, а не проверяет один из них:
// обёртку ставит конструктор рендерера, поэтому забыть снять её можно на
// каждом выходе по отдельности, и каждый такой отказ выглядит исправным —
// браузер обёртку выбрасывает молча. Появился шестой публичный выход —
// он обязан появиться в этой таблице.
func TestPublicOutputsCarryNoDocumentWrapper(t *testing.T) {
	r := NewRenderer()

	pageHTML, notes := r.CollectPages([]PageContent{{PageNumber: 1, Content: contractMarkdown}})
	scopedHTML, _ := r.CollectPagesScoped("v1-", []PageContent{{PageNumber: 1, Content: contractMarkdown}})

	outputs := map[string]string{
		"Render":             r.Render(contractMarkdown),
		"CollectPages":       pageHTML[0],
		"CollectPagesScoped": scopedHTML[0],
		"RenderNotes":        RenderNotes(notes),
		"RenderNotesWith":    RenderNotesWith(notes, NotesOptions{Inline: true}),
	}

	for name, got := range outputs {
		if strings.TrimSpace(got) == "" {
			t.Errorf("%s(): пусто — снятие обёртки не должно съедать текст", name)
			continue
		}
		for _, bad := range documentMarkers {
			if strings.Contains(got, bad) {
				t.Errorf("%s(): наружу уехал целый документ, найдено %q:\n%s", name, bad, got)
			}
		}
	}
}
