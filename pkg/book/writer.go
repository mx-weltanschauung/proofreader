package book

import (
	"io"
	"sort"
)

// Writer превращает книгу в файл одного формата.
type Writer interface {
	// Write пишет книгу в w. Реализации пишут потоком и ничего не копят в
	// памяти сверх необходимого: самый большой том корпуса — 907 страниц.
	Write(w io.Writer, b *Book) error
	// ContentType — значение заголовка Content-Type.
	ContentType() string
	// Ext — расширение файла, без точки.
	Ext() string
}

// writers — известные форматы. Заполняется по мере подключения писателей.
var writers = map[string]Writer{
	"md":   MarkdownWriter{},
	"html": HTMLWriter{},
	"epub": EPUBWriter{},
	"fb2":  FB2Writer{},
}

// ForFormat отдаёт писателя по имени формата из запроса.
func ForFormat(format string) (Writer, bool) {
	w, ok := writers[format]
	return w, ok
}

// Formats перечисляет известные форматы по алфавиту — для сообщения об
// ошибке, чтобы читателю было видно, что вообще можно попросить.
func Formats() []string {
	out := make([]string, 0, len(writers))
	for name := range writers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
