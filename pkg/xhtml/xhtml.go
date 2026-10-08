// Package xhtml снимает обёртку HTML-документа и сериализует узлы строгим
// XHTML.
//
// Пакет нейтральный: он не знает ни о базе, ни о HTTP, ни о разметке — только
// о дереве узлов. Поэтому его вправе импортировать и pkg/markdown, который
// обёртку ставит, и pkg/book, который собирает из фрагментов файлы для
// читалок. Обратные рёбра между теми двумя пакетами были бы хуже: pkg/markdown
// тянет internal/repository, и pkg/book потерял бы свойство «ничего не знает о
// базе и о HTTP», которым объявлен в шапке book.go.
package xhtml

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// voidElements — теги без содержимого. В HTML5 они пишутся <br>, в XHTML
// обязаны быть <br/>, иначе разбор XML спотыкается на первом же.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// Body отдаёт содержимое <body> отрендеренной страницы, заново
// сериализованное строгим XHTML.
//
// Рендерер разметки собран с флагом html.CompletePage
// (pkg/markdown/renderer.go), поэтому внутри пакета markdown каждая страница
// живёт целым документом XHTML 1.0 — с DOCTYPE, <head> и
// <meta name="GENERATOR">. Наружу такой документ не выходит: markdown снимает
// обёртку этой функцией на каждом публичном выходе. Браузер читальни обёртку
// молча выбрасывает, читалка EPUB показала бы пустую книгу.
//
// Разбор дерева нужен и по второй причине: флаг UseXHTML действует на
// разметку, которую генерирует gomarkdown, но не на HTML, вставленный в
// content_markdown руками — а он там есть (таблицы примечаний, см.
// pkg/markdown/html_block_notes.go). Сырой <br> из тела страницы приезжает
// незакрытым. Строковой обработкой это не чинится.
//
// Ошибку функция возвращает, хотя ветка сегодня недостижима: html.Parse
// отдаёт ошибку только от io.Reader, а источник здесь всегда
// strings.NewReader. Возврат сохранён нарочно — если источником однажды
// станет не строка, зовущие обязаны это увидеть.
func Body(rendered string) (string, error) {
	if strings.TrimSpace(rendered) == "" {
		return "", nil
	}
	doc, err := html.Parse(strings.NewReader(rendered))
	if err != nil {
		return "", fmt.Errorf("разбор отрендеренной страницы: %w", err)
	}
	body := FindBody(doc)
	if body == nil {
		return "", nil
	}
	var b strings.Builder
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		WriteNode(&b, c)
	}
	return strings.TrimSpace(b.String()), nil
}

// FindBody walks the tree rooted at n and returns the first <body> element
// node found, or nil if none exists.
func FindBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == atom.Body {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := FindBody(c); found != nil {
			return found
		}
	}
	return nil
}

// WriteNode сериализует узел строгим XHTML. Именованные сущности сюда уже не
// доходят: парсер превратил их в руны, а руны пишутся как UTF-8 — валидный
// XML без единой сущности, кроме пяти обязательных.
func WriteNode(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(EscapeText(n.Data))
		return
	case html.ElementNode:
		// продолжаем ниже
	default:
		// Комментарии, doctype и прочее в выгрузку не едут.
		return
	}

	b.WriteByte('<')
	b.WriteString(n.Data)
	for _, a := range n.Attr {
		// Обработчик ошибок HTML5-парсера умеет сворачивать беглую кавычку
		// внутри значения атрибута в отдельный атрибут, чьё имя — обрывок
		// исходного текста (пробелы, кавычки, «=» внутри). Такое имя не
		// проходит ни под одну грамматику XML Name — чинить его нечем, автор
		// не имел в виду атрибут с таким именем, поэтому вырезаем целиком.
		if !isValidXMLName(a.Key) {
			continue
		}
		if a.Namespace != "" && !isValidXMLName(a.Namespace) {
			continue
		}
		b.WriteByte(' ')
		if a.Namespace != "" {
			b.WriteString(a.Namespace)
			b.WriteByte(':')
		}
		b.WriteString(a.Key)
		b.WriteString(`="`)
		b.WriteString(EscapeAttr(a.Val))
		b.WriteByte('"')
	}
	if voidElements[n.Data] {
		b.WriteString("/>")
		return
	}
	b.WriteByte('>')
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		WriteNode(b, c)
	}
	b.WriteString("</")
	b.WriteString(n.Data)
	b.WriteByte('>')
}

// isValidXMLName — упрощённая проверка продукции XML Name: первый символ —
// буква или подчёркивание, дальше — буквы, цифры, дефис, точка или
// подчёркивание. Пустая строка не проходит. Полная грамматика XML 1.0 шире
// (там ещё двоеточия, комбинирующие знаки и диапазоны по кодовым точкам),
// но для имени атрибута этого достаточно — цель проверки не соответствие
// стандарту вплоть до буквы, а отсеять мусор вроде кавычек и пробелов,
// которые обработчик ошибок парсера может затащить в Key.
func isValidXMLName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case i == 0:
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.' || r == '_':
			// допустимо
		default:
			return false
		}
	}
	return true
}

// isDisallowedXMLControl — управляющий байт, который XML 1.0 запрещает в
// каком угодно виде (§2.2, продукция Char). Разрешены только таб, LF и CR;
// остальные ниже 0x20 — это шум OCR-скана, а не текст, и физически не могут
// доехать до EPUB-читалки: она строгий XML-парсер, споткнётся на первом же.
func isDisallowedXMLControl(c byte) bool {
	return c < 0x20 && c != '\t' && c != '\n' && c != '\r'
}

// EscapeText экранирует текстовый узел и вырезает запрещённые управляющие
// байты. Вырезает, а не подменяет пробелом — это мусор сканера, а не разделитель,
// подмена пробелом рискует расклеить слово надвое.
func EscapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '&':
			b.WriteString("&amp;")
		case c == '<':
			b.WriteString("&lt;")
		case c == '>':
			b.WriteString("&gt;")
		case isDisallowedXMLControl(c):
			// пропускаем — см. isDisallowedXMLControl
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// EscapeAttr экранирует значение атрибута. Таб, CR и LF здесь — не литералы:
// по XML 1.0 §3.3.3 любой конформный парсер при чтении молча схлопывает их в
// пробел, а конкретно CR схлопывается ещё на входном препроцессинге. Пишем их
// числовыми ссылками, чтобы значение пережило обход через строгий парсер
// без изменений. В тексте (EscapeText) это не нужно — там все три обычные.
func EscapeAttr(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '&':
			b.WriteString("&amp;")
		case c == '<':
			b.WriteString("&lt;")
		case c == '>':
			b.WriteString("&gt;")
		case c == '"':
			b.WriteString("&quot;")
		case c == '\t':
			b.WriteString("&#9;")
		case c == '\n':
			b.WriteString("&#10;")
		case c == '\r':
			b.WriteString("&#13;")
		case isDisallowedXMLControl(c):
			// пропускаем — см. isDisallowedXMLControl
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
