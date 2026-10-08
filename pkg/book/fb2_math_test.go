package book

import (
	"strings"
	"testing"
)

func fb2WithPage(t *testing.T, pageHTML string) string {
	t.Helper()
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML = pageHTML
	return writeString(t, FB2Writer{}, b)
}

func TestFB2СхемаСтановитсяТаблицей(t *testing.T) {
	got := fb2WithPage(t, mathPageHTML)

	if !strings.Contains(got, "<table>") {
		t.Fatalf("схема не стала таблицей:\n%s", got)
	}
	if !strings.Contains(got, "Капитал = 7500") || !strings.Contains(got, "Продукт = 9000") {
		t.Error("итоги схемы потеряны")
	}
	if !strings.Contains(got, "}") {
		t.Error("фигурная скобка потеряна")
	}
	if strings.Contains(got, `\left.`) || strings.Contains(got, `\begin`) {
		t.Error("во FB2 уехал сырой LaTeX вместо понижения")
	}
}

func TestFB2НеПечатаетФормулуДважды(t *testing.T) {
	// Существующий дефект: обходчик берёт и презентационную ветвь MathML, и
	// содержимое <annotation encoding="application/x-tex">, отчего «c+v»
	// превращалось в «c+vc+v».
	got := fb2WithPage(t, `<p><span class="math inline">\(c+v\)</span></p>`)

	if strings.Contains(got, "c+vc+v") {
		t.Fatalf("формула напечатана дважды:\n%s", got)
	}
	if !strings.Contains(got, "c+v") {
		t.Errorf("формула потеряна:\n%s", got)
	}
}

func TestFB2СтепеньСтановитсяSup(t *testing.T) {
	got := fb2WithPage(t, `<p>Формула: <span class="math inline">\(a^2\)</span>.</p>`)

	if !strings.Contains(got, "<sup>2</sup>") {
		t.Errorf("показатель степени не стал <sup>:\n%s", got)
	}
}

func TestFB2НеподъёмнаяФормулаОстаётсяTeX(t *testing.T) {
	// Понизить половину и замолчать остальное хуже, чем показать исходник:
	// читатель видит, что здесь формула, и видит какая.
	got := fb2WithPage(t, `<p><span class="math display">\[\sqrt{a^2+b^2}\]</span></p>`)

	if !strings.Contains(got, `\sqrt{a^2+b^2}`) {
		t.Errorf("откат на строку TeX не сработал:\n%s", got)
	}
	if strings.Contains(got, "<table>") {
		t.Error("неподъёмная формула не должна превращаться в таблицу")
	}
}

func TestFB2СФормуламиПроходитСхему(t *testing.T) {
	got := fb2WithPage(t, mathPageHTML)
	validateFB2Schema(t, got)
}

func TestFB2ОдиночныйМассивСтановитсяТаблицей(t *testing.T) {
	// Формула без обрамляющих \left.…\right\} — сам \begin{array} и есть вся
	// презентационная ветвь (mathPresentation отдаёт <mtable> напрямую, а не
	// <mrow> с <mtable> внутри). fb2MathTable раньше умел находить mtable
	// только среди детей pres, а не в самом pres — такая формула откатывалась
	// на сырой TeX.
	got := fb2WithPage(t, `<p><span class="math display">\[\begin{array}{l}a=1\\b=2\end{array}\]</span></p>`)

	if !strings.Contains(got, "<table>") {
		t.Fatalf("одиночный массив не стал таблицей:\n%s", got)
	}
	if strings.Contains(got, `\begin{array}`) {
		t.Errorf("во FB2 уехал сырой LaTeX вместо понижения:\n%s", got)
	}
	if strings.Count(got, "<tr>") != 2 {
		t.Errorf("ожидались две строки таблицы (a=1, b=2), получили:\n%s", got)
	}
	validateFB2Schema(t, got)
}

func TestFB2ПустойМассивОстаётсяTeX(t *testing.T) {
	// \begin{array}{l}\end{array} — mtable без единой <mtr>. Если это не
	// отловить, получится <table></table> без <tr> — невалидный FB2
	// (SCHEMAV_ELEMENT_CONTENT: Element 'table': Missing child element(s).
	// Expected is (tr)), валящий схему всего тома, а не только этой формулы.
	got := fb2WithPage(t, `<p><span class="math display">\[\begin{array}{l}\end{array}\]</span></p>`)

	if strings.Contains(got, "<table>") {
		t.Errorf("пустой массив не должен превращаться в таблицу:\n%s", got)
	}
	if !strings.Contains(got, `\begin{array}{l}\end{array}`) {
		t.Errorf("откат на строку TeX не сработал:\n%s", got)
	}
	validateFB2Schema(t, got)
}
