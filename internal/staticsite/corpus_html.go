package staticsite

import (
	"regexp"
	"strings"
)

// Вёрстка текста корпуса несёт два вида ссылок в никуда. На боевом они
// безвредны (их просто никто не нажимает), а самопроверка статической копии
// ловит их — и копия обязана их не нести.

// fnBack — обратная ссылка из списка примечаний к месту в тексте. У
// примечания без места в тексте этой полосы (список «Примечаний» тома: текст
// примечания стоит там, а ссылки на него — в главах) она ведёт на fnref,
// которого в вёрстке нет; рендерер это знает и считает «inert by
// construction» (pkg/markdown/notes.go). В копии такая ссылка становится
// номером без ссылки.
var fnBack = regexp.MustCompile(`<a class="fn-back" href="#(fnref:[^"]+)">([^<]*)</a>`)

// corpusLink — ссылка, которую markdown собрал из текста корпуса: печатная
// вставка в квадратных скобках, за которой вплотную идёт круглая скобка
// («[декабря](пишу это…)»). Настоящих ссылок в корпусе нет ни одной (замер
// 06.10.2026: 0 полос с «](/» или «](http»), а у такой «ссылки» текст в
// скобках уходит в href и читателю не виден. Копия возвращает его в строку
// так, как он записан в тексте полосы.
var corpusLink = regexp.MustCompile(`<a href="([^"#][^"]*)"[^>]*>(.*?)</a>`)

// cleanCorpusHTML снимает обе ссылки в никуда с вёрстки текста одного файла.
// Зовётся только для текста корпуса, не для оболочки и не для разборов.
func cleanCorpusHTML(html string) string {
	return corpusLink.ReplaceAllString(dropInertBackLinks(html), "[$2]($1)")
}

// dropInertBackLinks печатает номером обратную ссылку примечания, чьего
// места в тексте в этой вёрстке нет. Общая для текста корпуса и разборов
// (вклейки несут вёрстку корпуса).
func dropInertBackLinks(html string) string {
	return fnBack.ReplaceAllStringFunc(html, func(m string) string {
		sub := fnBack.FindStringSubmatch(m)
		if strings.Contains(html, `id="`+sub[1]+`"`) {
			return m
		}
		return `<span class="fn-back">` + sub[2] + `</span>`
	})
}
