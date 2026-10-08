package staticsite

import (
	"encoding/json"
	"strings"
)

// titleEntry — строка поиска по заглавиям: вид, заглавие, подпись, путь от
// корня. Массив, а не объект: на 30 тысяч записей имена полей удвоили бы файл.
type titleEntry [4]string

func (g *Generator) addTitle(kind, title, sub, path string) {
	g.titles = append(g.titles, titleEntry{kind, OneLine(title), OneLine(sub), path})
}

// writeSearch пишет индекс заглавий и страницу поиска. Индекс — скрипт с
// присваиванием, а не JSON: через file:// браузер не даёт читать файлы
// fetch'ем, а тег script грузится.
func (g *Generator) writeSearch() error {
	data, err := json.Marshal(g.titles)
	if err != nil {
		return err
	}
	if err := g.write(assetTitles, "window.TITLES="+string(data)+";\n"); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("<h1>Поиск</h1>\n")
	b.WriteString(`<form method="get"><input id="q" type="search" name="q" aria-label="Запрос"> <button type="submit">Искать</button></form>` + "\n")
	b.WriteString(`<noscript><p class="hint">Поиск работает со включённым JavaScript.</p></noscript>` + "\n")
	b.WriteString(`<section id="titles"></section>` + "\n")
	b.WriteString(`<section><select id="edition" hidden><option value="">Все издания</option></select><div id="fulltext"></div></section>` + "\n")
	return g.write(SearchFile, g.page(shell{
		path: SearchFile, title: "Поиск", body: b.String(), prod: "/search",
		scripts: []string{assetTitles, assetSearch, assetSearchPage},
	}))
}
