package markdown

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// unsafeMarkupIn разбирает фрагмент HTML и перечисляет приметы недоверенного,
// найденные В РАЗМЕТКЕ: обработчики событий, style, небезопасные схемы в
// адресах, опасные элементы и элементы, ТЯНУЩИЕ ВНЕШНИЙ ПОДРЕСУРС.
//
// Последнее — не про исполнение, а про маячок: `<img src="https://чужой/…">`
// в теле разбора заставляет браузер модератора сходить на чужой хост, и
// обязательный просмотр превращается в отметку о том, кто и когда читал,
// выданную тому, кого рассматривают. Проверка по схеме адреса этого не ловит
// (`https` безопасен), поэтому судим по самому элементу. Функция применяется
// ТОЛЬКО к выводу авторского текста разбора: у корпуса картинки законны, и
// его вывод сюда не заходит.
//
// Разбор, а не поиск подстроки, — и это не педантизм, а урок повторной
// рецензии. Подавление блочных атрибутов (parser.Attributes) оставляет строку
// `{onclick="alert(1)"}` в выводе ТЕКСТОМ абзаца: экранированным, безвредным
// и по-прежнему содержащим подстроку «onclick». Сторож на подстроке после
// верной починки кричал бы на безобидное, а до починки — путал бы текст с
// атрибутом. Судить надо по свойству, а не по буквам.
//
// Твин этой функции живёт в internal/api/executable_markup_probe_test.go: Go не
// умеет делить тестовые помощники между пакетами, а обе стороны шва обязаны
// судить об исполняемом одинаково.
func unsafeMarkupIn(fragment string) []string {
	nodes, err := html.ParseFragment(strings.NewReader(fragment),
		&html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return []string{fmt.Sprintf("фрагмент не разобрался: %v", err)}
	}

	found := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "iframe", "object", "embed", "form", "base":
				found["<"+n.Data+">"] = true
			case "img", "picture", "source", "video", "audio", "track", "link":
				// Внешний подресурс: маячок, выдающий адрес и клиента того,
				// кто открыл предпросмотр. См. докблок выше.
				found["<"+n.Data+"> (внешний подресурс)"] = true
			}
			for _, a := range n.Attr {
				name := strings.ToLower(a.Key)
				switch {
				case strings.HasPrefix(name, "on"):
					found[n.Data+"["+name+"]"] = true
				case name == "style":
					found[n.Data+"[style]"] = true
				case name == "href" || name == "src" || name == "srcset" ||
					name == "action" || name == "formaction" || name == "data":
					if scheme := urlScheme(a.Val); scheme != "" && !safeScheme[scheme] {
						found[n.Data+"["+name+"="+scheme+":]"] = true
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}

	out := make([]string, 0, len(found))
	for k := range found {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var safeScheme = map[string]bool{"http": true, "https": true, "mailto": true}

// urlScheme отдаёт схему адреса в нижнем регистре или "" у относительного
// (`/works/1`, `#fn:1`) — относительные адреса безопасны и к ним претензий
// нет. Пробелы и управляющие знаки внутри схемы срезаются: `java\tscript:`
// браузер понимает, а наивное сравнение — нет.
func urlScheme(raw string) string {
	trimmed := strings.Map(func(r rune) rune {
		if r <= ' ' {
			return -1
		}
		return r
	}, raw)
	i := strings.IndexByte(trimmed, ':')
	if i <= 0 {
		return ""
	}
	scheme := strings.ToLower(trimmed[:i])
	for _, r := range scheme {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') &&
			r != '+' && r != '-' && r != '.' {
			return "" // не схема, а двоеточие в тексте адреса
		}
	}
	return scheme
}
