package book

import (
	"strings"
	"testing"
)

// mathPageHTML — то, что markdown-рендерер (parser.MathJax) оставляет в
// Page.HTML: блочная формула в \[…\], строчная в \(…\).
const mathPageHTML = `<p><span class="math display">\[\left.\begin{array}{l}` +
	`\text{I } 4000\,c = 6000\\\text{II } 2000\,c = 3000\end{array}\right\}` +
	`\begin{array}{l}\text{Капитал} = 7500\\\text{Продукт} = 9000\end{array}\]</span></p>` +
	`<p>Строчная: <span class="math inline">\(I(v+m) = II\,c\)</span>.</p>`

func TestWithMathMLПодставляетРазметку(t *testing.T) {
	got := withMathML(mathPageHTML)

	if strings.Contains(got, `\[`) || strings.Contains(got, `\(`) {
		t.Error("разделители LaTeX остались в выводе")
	}
	for _, want := range []string{"<math", "<mtable", `<mo fence="true">}</mo>`,
		`xmlns="http://www.w3.org/1998/Math/MathML"`} {
		if !strings.Contains(got, want) {
			t.Errorf("в выводе нет %q", want)
		}
	}
	if strings.Count(got, "<math") != 2 {
		t.Errorf("ожидались две формулы, найдено %d", strings.Count(got, "<math"))
	}
}

func TestWithMathMLОставляетБитуюФормулуКакЕсть(t *testing.T) {
	// Одна опечатка не должна ронять скачивание тома на 900 страниц.
	in := `<p><span class="math inline">\(\frac{1\)</span></p>`
	got := withMathML(in)

	if strings.Contains(got, "<math") {
		t.Error("битая формула не должна превращаться в MathML")
	}
	if !strings.Contains(got, `\frac{1`) {
		t.Errorf("исходный LaTeX потерян: %s", got)
	}
}

func TestWithMathMLНеТрогаетСтраницыБезФормул(t *testing.T) {
	in := `<p>Обычный абзац со <em>значением</em>.</p>`
	if got := withMathML(in); got != in {
		t.Errorf("страница без формул изменилась:\nбыло: %s\nстало: %s", in, got)
	}
}

func TestHTMLПисательОтдаётMathML(t *testing.T) {
	b := sampleBook()
	b.Sections[0].Blocks[0].Pages[0].HTML = mathPageHTML

	got := writeString(t, HTMLWriter{}, b)

	if !strings.Contains(got, "<math") {
		t.Error("в HTML нет MathML")
	}
	if strings.Contains(got, `\[`) {
		t.Error("в HTML остался сырой LaTeX")
	}
}
