package book

import "time"

// Book — то, что уедет в файл: выходные данные и дерево секций.
type Book struct {
	Meta     Meta
	Sections []Section
}

// Meta — выходные данные книги.
//
// У подборки Edition, Volume, PageFrom и PageTo пусты: он собран из разных
// томов, единого диапазона у него нет. Выходные данные источников в этом
// случае печатаются построчно у каждого элемента оглавления.
type Meta struct {
	Title     string
	Authors   []string
	Edition   string // «К. Маркс и Ф. Энгельс. Сочинения»
	Volume    string // «Том 23»
	PageFrom  int    // печатные границы
	PageTo    int
	URL       string    // этот же материал в читальне
	Modified  time.Time // max(updated_at) страниц — не время скачивания
	Lang      string    // "ru"
	Proofread Proofread
	// CacheKey — то, что на самом деле идёт в ETag (internal/api/download_handler.go).
	// Modified выше отвечает только за текст страниц и печатается на титуле —
	// расширять его смысл нельзя, иначе "Состояние текста на …" начнёт
	// врать про правки, к тексту не относящиеся. CacheKey — то же самое
	// значение плюс UpdatedAt сущностей, от которых зависят байты файла, но
	// не его текст: работы (смещение печатной нумерации), главы (своей и
	// работы — переименование, перенос), подборки и его пунктов
	// (переименование, состав, порядок). Внутрь файла не попадает.
	CacheKey time.Time
	// Missing — элементы подборки, чей источник удалён. Молча терять пункт
	// оглавления нельзя, поэтому они перечисляются на титуле.
	Missing []string
}

// Proofread — состояние вычитки. Пустые страницы в Total не входят.
type Proofread struct {
	Total   int
	Human   int
	Machine int
	Raw     int
}

// Section — узел оглавления вместе со своим текстом.
type Section struct {
	Title  string
	Author string // непусто только там, где отличается от книги (подборка)
	// ChapterID — id главы читальни, из которой собрана секция; 0 — секция
	// не глава (безымянный хвост тома, элемент подборки, выгрузки вообще).
	// Заполняет только DownloadSource.WithChapterAnchors() для статической
	// читальни: тогда якорем секции становится ch-<id> (ChapterAnchor). EPUB
	// это поле не читает — его оглавление и имена файлов позиционные, — и
	// выгрузки его не заполняют, поэтому их файлы прежние байт в байт
	// (TestBodyHTMLWithoutChapterIDsIsUnchanged).
	ChapterID int64
	Blocks    []Block
	// NotesHTML — блок примечаний секции. Заполняется только у секций верхнего
	// уровня: CollectPages перенумеровывает подстрочные сквозь весь
	// переданный кусок, и на томе в 907 страниц сквозная нумерация дала бы
	// (1)…(1500) одной простынёй.
	NotesHTML string
}

// Block — либо прогон собственных страниц секции, либо вложенная секция.
// Ровно одно из полей непусто.
type Block struct {
	Pages []Page
	Child *Section
}

// Page — одна страница книги.
//
// Internal — номер в базе, по нему ложатся границы глав. Printed — номер на
// бумаге (Internal + works.page_offset), он и попадает в выгрузку. В подборке
// смещение у каждого элемента своё.
type Page struct {
	Internal int
	Printed  int
	HTML     string // содержимое <body>, уже прошедшее через xhtml.Body (снимается fragment() в pkg/markdown)
	Markdown string // сырой content_markdown
}

// Pages — страницы одного источника, отсортированные по Internal.
type Pages []Page

// Range отдаёт страницы с внутренними номерами from..to включительно.
// Отсутствующие в базе номера пропускаются, а не выдумываются.
func (p Pages) Range(from, to int) []Page {
	var out []Page
	for _, page := range p {
		if page.Internal >= from && page.Internal <= to {
			out = append(out, page)
		}
	}
	return out
}

// PageCount — сколько страниц в книге всего. Нужен отдаче: пустую книгу
// незачем упаковывать.
func (b *Book) PageCount() int {
	n := 0
	for _, s := range b.Sections {
		n += sectionPageCount(s)
	}
	return n
}

func sectionPageCount(s Section) int {
	n := 0
	for _, b := range s.Blocks {
		if b.Child != nil {
			n += sectionPageCount(*b.Child)
			continue
		}
		n += len(b.Pages)
	}
	return n
}
