package staticsite

import (
	"strings"
	"testing"
)

func TestCleanCorpusHTMLDropsInertBackLinks(t *testing.T) {
	in := `<sup class="footnote-ref" id="fnref:s0-1-1"><a href="#fn:s0-1-1">1</a></sup>` +
		`<li id="fn:s0-1-1"><a class="fn-back" href="#fnref:s0-1-1">1</a> живая</li>` +
		`<li id="fn:s0-9-2"><a class="fn-back" href="#fnref:s0-9-2">2</a> сирота</li>`
	got := cleanCorpusHTML(in)
	if !strings.Contains(got, `<a class="fn-back" href="#fnref:s0-1-1">1</a>`) {
		t.Errorf("обратная ссылка на место в тексте пропала:\n%s", got)
	}
	if !strings.Contains(got, `<span class="fn-back">2</span> сирота`) || strings.Contains(got, `#fnref:s0-9-2`) {
		t.Errorf("обратная ссылка примечания без места в тексте осталась ссылкой в никуда:\n%s", got)
	}
}

func TestCleanCorpusHTMLRestoresBracketText(t *testing.T) {
	in := `<p>5-го <a href="пишу это 6-го в 91/4 утра" target="_blank">декабря</a>. — Утром` +
		`<sup><a href="#fn:x">1</a></sup></p>`
	got := cleanCorpusHTML(in)
	if !strings.Contains(got, `5-го [декабря](пишу это 6-го в 91/4 утра). — Утром`) {
		t.Errorf("текст в скобках не вернулся в строку:\n%s", got)
	}
	if !strings.Contains(got, `<a href="#fn:x">1</a>`) {
		t.Errorf("ссылка на сноску внутри страницы пострадала:\n%s", got)
	}
}

func TestChapterFileHasNoInertLinks(t *testing.T) {
	src := fixture()
	src.volumes[100].Sections[2].NotesHTML = `<li id="fn:s2-9-1"><a class="fn-back" href="#fnref:s2-9-1">1</a> сирота</li>`
	src.volumes[100].Sections[2].Blocks[0].Pages[0].HTML = `<p>Конец <a href="в скобках" target="_blank">слова</a>.</p>`
	out := build(t, src)
	body := read(t, out, ch12)
	if strings.Contains(body, `href="#fnref:s2-9-1"`) || strings.Contains(body, `href="в скобках"`) {
		t.Errorf("в файле главы остались ссылки в никуда")
	}
}
