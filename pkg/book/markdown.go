package book

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

// MarkdownWriter отдаёт исходный текст корпуса одним файлом .md.
//
// Единственный писатель мимо HTML-оси: он берёт content_markdown как есть,
// вместе с родными сносками разметки. Формат для работы с текстом — цитировать,
// грепать, обрабатывать своим инструментом, — а не для чтения в читалке.
//
// Текст страницы попадает в выгрузку буквально, без экранирования: если он
// начинается с "#", получится заголовок верхнего уровня внутри секции. Это
// осознанный выбор, а не недосмотр — формат существует именно для того,
// чтобы отдавать исходный текст неизменным, и экранирование сломало бы то
// единственное свойство, ради которого его выбирают.
type MarkdownWriter struct{}

func (MarkdownWriter) ContentType() string { return "text/markdown; charset=utf-8" }
func (MarkdownWriter) Ext() string         { return "md" }

func (MarkdownWriter) Write(w io.Writer, b *Book) error {
	text, _ := MarkdownWithPageStarts(b)
	_, err := io.WriteString(w, text)
	return err
}

// PageStart — полоса в тексте MarkdownWithPageStarts.
//
// Offset — байтовое смещение, с которого может начаться кусок текста,
// несущий эту полосу: её маркер «[N]», а если прямо перед ним стоят
// заголовки секций — начало первого из них. Резать по самому маркеру
// нельзя: заголовок вложенной главы пишется перед её первой полосой и уехал
// бы в хвост предыдущего куска, отдельно от своего текста.
type PageStart struct {
	Offset  int
	Printed int
}

// markdownOut — вывод писателя и то, что о нём нужно резчику на части.
type markdownOut struct {
	strings.Builder
	starts []PageStart
	// heading — смещение первого заголовка, ещё не накрытого полосой; -1 — нет.
	heading int
}

// MarkdownWithPageStarts — текст, который пишет MarkdownWriter, и начала
// полос в нём. Отдельной функцией ради текста главы для нейросети
// (internal/seo): длинную главу он режет на части и обязан резать по
// полосам, а искать маркер «[N]» в готовом тексте нельзя — такая строка
// законна и в самом тексте корпуса.
func MarkdownWithPageStarts(b *Book) (string, []PageStart) {
	out := &markdownOut{heading: -1}

	out.WriteString("# " + oneLine(b.Meta.Title) + "\n\n")
	// TitleLines сама несёт название второй строкой (после авторов) — общий
	// титул для HTML/EPUB/FB2, у которых отдельного заголовка перед ним нет.
	// Здесь заголовок уже напечатан строкой "#" выше, и без вычёркивания
	// книга открывалась бы названием дважды подряд.
	for _, line := range dropFirstEqual(TitleLines(b.Meta), b.Meta.Title) {
		if line == "" {
			out.WriteString("\n")
			continue
		}
		out.WriteString(line + "\n")
	}
	out.WriteString("\n")

	for _, s := range b.Sections {
		writeMarkdownSection(out, s, 2)
	}
	return out.String(), out.starts
}

// writeMarkdownSection печатает секцию и её потомков. level — уровень
// заголовка markdown; глубже шестого markdown не умеет, поэтому упирается.
func writeMarkdownSection(out *markdownOut, s Section, level int) {
	if level > 6 {
		level = 6
	}
	if out.heading < 0 {
		out.heading = out.Len()
	}
	out.WriteString(strings.Repeat("#", level) + " " + oneLine(s.Title) + "\n\n")
	if s.Author != "" {
		out.WriteString("*" + oneLine(s.Author) + "*\n\n")
	}

	for _, block := range s.Blocks {
		if block.Child != nil {
			writeMarkdownSection(out, *block.Child, level+1)
			continue
		}
		for _, p := range block.Pages {
			at := out.Len()
			if out.heading >= 0 {
				at = out.heading
				out.heading = -1
			}
			out.starts = append(out.starts, PageStart{Offset: at, Printed: p.Printed})
			fmt.Fprintf(out, "[%d]\n\n", p.Printed)
			body := strings.TrimSpace(p.Markdown)
			if body == "" {
				continue
			}
			out.WriteString(prefixPageFootnotes(body, p.Printed) + "\n\n")
		}
	}
}

// oneLine схлопывает переводы строк: заголовок не должен разрывать строку
// markdown-заголовка. Тот же приём, что в pkg/export.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)), " ")
}

// dropFirstEqual убирает первую строку среза, точно равную s, — ровно одну.
// Пустой s не трогает ничего: у него слишком много случайных совпадений
// (пустые строки-разделители TitleLines), чтобы угадывать нужную.
func dropFirstEqual(lines []string, s string) []string {
	if s == "" {
		return lines
	}
	for i, line := range lines {
		if line == s {
			out := make([]string, 0, len(lines)-1)
			out = append(out, lines[:i]...)
			out = append(out, lines[i+1:]...)
			return out
		}
	}
	return lines
}

// footnoteNameRe соответствует и ссылке на сноску, и её определению —
// синтаксически это одно и то же "[^имя]", определение отличается только
// идущим следом ":". Тот же паттерн, что prefixFootnotesWithPageNumber в
// pkg/markdown/renderer.go.
var footnoteNameRe = regexp.MustCompile(`\[\^([^\]]+)\]`)

// fenceRe узнаёт строку, открывающую или закрывающую отгороженный блок
// кода: три и более одинаковых символа "`" или "~", с необязательным
// отступом впереди.
var fenceRe = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// prefixPageFootnotes переименовывает сноски одной страницы, добавляя к
// имени префикс "{printed}-": [^1] на печатной странице 101 становится
// [^101-1], и ссылка, и определение — тем же приёмом, что
// prefixFootnotesWithPageNumber в pkg/markdown/renderer.go для HTML.
//
// Без этого шага имена сносок остаются "page-local by construction": две
// разные страницы обычной книги нередко обе определяют "[^1]" — при простой
// склейке текста страниц друг за другом это два разных определения одного
// и того же имени в одном документе, и любой сноскочитающий рендерер
// разрешит обе ссылки на то определение, которое подвернётся ему первым.
//
// Имя сноски — не обязательно число: "[^r2]"/"[^s3]" тоже встречаются в
// корпусе (страницевые примечания переводчика и редакторские отметки
// astérisk-сносок), поэтому префикс приклеивается к имени целиком, а не
// парсится как число.
//
// Строки внутри отгороженных блоков кода (```…``` или ~~~…~~~) не трогаем:
// показанный там "[^1]" — буквальный пример разметки, а не настоящая сноска.
func prefixPageFootnotes(markdown string, printed int) string {
	prefix := fmt.Sprintf("%d-", printed)
	lines := strings.Split(markdown, "\n")

	inFence := false
	var fenceChar byte
	var fenceLen int

	for i, line := range lines {
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			marker := m[1]
			switch {
			case !inFence:
				inFence = true
				fenceChar = marker[0]
				fenceLen = len(marker)
			case marker[0] == fenceChar && len(marker) >= fenceLen:
				inFence = false
			}
			continue // сама строка изгороди сносок не содержит
		}
		if inFence {
			continue
		}
		lines[i] = footnoteNameRe.ReplaceAllString(line, "[^"+prefix+"$1]")
	}
	return strings.Join(lines, "\n")
}
