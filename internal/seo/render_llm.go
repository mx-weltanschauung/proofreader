package seo

import (
	"context"
	"errors"
	"fmt"
	"proofreader/internal/site"
	"strings"
	"time"
	"unicode/utf8"

	"proofreader/internal/models"
	"proofreader/pkg/book"
)

// Текст для нейросетей: .md-близнецы главы и тома и /llms.txt. Спека —
// docs/superpowers/specs/2026-09-29-chapter-for-llm-design.md.
//
// Читатель даёт ссылку своему чату (ChatGPT, Claude, …), сборщик чата
// забирает страницу. HTML главы у сборщика тоже есть (он входит в карту
// краулеров nginx), но длинную главу чат обрежет молча; .md режется на части
// сам и говорит модели, где продолжение.

// TextDoc — простой текст для отдачи. Canonical — абсолютный адрес
// HTML-страницы того же материала (идёт в заголовок Link); пусто — заголовка
// нет. NoIndex — заголовок X-Robots-Tag: .md глав и томов — дубли страниц
// читальни, в поиске им не место.
type TextDoc struct {
	Body      string
	Canonical string
	NoIndex   bool
	CacheKey  time.Time
}

// llmPartRunes — предел части в знаках. 80 тыс. знаков кириллицы — около
// 30—40 тыс. токенов. Порог, за которым чаты обрезают страницу, не мерен;
// значение уточняется замером после выкатки (спека, «После выкатки»).
const llmPartRunes = 80000

// errNoSuchPart — номер части за пределами главы. Форма адреса верна, а части
// нет: 404, а не 410 — «была и снята» тут не про что.
var errNoSuchPart = errors.New("такой части нет")

// LLMReadingNote — как модели читать текст. Одна строка на все части и главы.
// Экспортирована: шапка полос MCP-сервера говорит модели то же самое.
const LLMReadingNote = "> Как читать: [123] перед абзацами — номер страницы печатного издания,\n" +
	"> ссылайтесь на него. Сноски помечены [^123-1], их текст — на той же странице."

// textPart — кусок текста главы и печатные номера его первой и последней
// полосы (0 — полос в куске нет, так бывает только у главы без полос).
type textPart struct {
	Body                   string
	FromPrinted, ToPrinted int
}

// splitParts режет текст по началам полос так, чтобы часть не выходила за
// limit знаков. Полосу не режет никогда: полоса больше предела — отдельная
// часть целиком. Титул до первой полосы едет в первую часть. Склейка тел всех
// частей — ровно text.
func splitParts(text string, starts []book.PageStart, limit int) []textPart {
	return splitPartsFirst(text, starts, limit, limit)
}

// splitPartsFirst — splitParts с отдельным пределом первой части: у понятия
// в часть 1 ещё входит оглавление (render_concept_llm.go).
func splitPartsFirst(text string, starts []book.PageStart, first, limit int) []textPart {
	if len(starts) == 0 {
		return []textPart{{Body: text}}
	}
	var parts []textPart
	cur := textPart{}
	from := 0
	runes := utf8.RuneCountInString(text[:starts[0].Offset])
	hasPage := false
	for i, st := range starts {
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1].Offset
		}
		seg := utf8.RuneCountInString(text[st.Offset:end])
		max := limit
		if len(parts) == 0 {
			max = first
		}
		if hasPage && runes+seg > max {
			cur.Body = text[from:st.Offset]
			parts = append(parts, cur)
			cur = textPart{}
			from = st.Offset
			runes = 0
			hasPage = false
		}
		if !hasPage {
			cur.FromPrinted = st.Printed
		}
		cur.ToPrinted = st.Printed
		runes += seg
		hasPage = true
	}
	cur.Body = text[from:]
	return append(parts, cur)
}

// chapterTextPath — адрес текста главы: первая часть живёт по адресу самой
// главы с .md, остальные — /part-N.md под ним.
func chapterTextPath(chapter string, part int) string {
	if part <= 1 {
		return chapter + ".md"
	}
	return fmt.Sprintf("%s/part-%d.md", chapter, part)
}

// ChapterText — часть part (с единицы) текста главы. Обёртка над
// ChapterTextParts для тех, кому нужна одна часть; обработчик берёт все
// части разом (handler.go).
func (s *Source) ChapterText(ctx context.Context, workID, chapterID int64, part int) (*TextDoc, error) {
	_, docs, err := s.ChapterTextParts(ctx, workID, chapterID)
	if err != nil {
		return nil, err
	}
	if part < 1 || part > len(docs) {
		return nil, errNoSuchPart
	}
	return docs[part-1], nil
}

// ChapterTextParts — все части текста главы разом: docs[k-1] — часть k.
// chapter — путь главы без домена (адрес k-й части — chapterTextPath).
// Книга та же, что у скачивания и у HTML-страницы (Books.Chapter), текст —
// вывод MarkdownWriter байт в байт.
//
// Разом, а не по части: одна часть стоит рендера всей главы (книга, markdown,
// разрез), и глава в 28 частей, прочитанная подряд, стоила бы 28 полных
// рендеров вместо одного.
func (s *Source) ChapterTextParts(ctx context.Context, workID, chapterID int64) (string, []*TextDoc, error) {
	b, err := s.Books.Chapter(ctx, workID, chapterID)
	if err != nil {
		return "", nil, err
	}
	// Путь главы — из строк главы и тома, как в Chapter() (render_text.go), а
	// не разбором b.Meta.URL: там абсолютный адрес, собранный копией
	// построителя в internal/api, а адреса частей обязаны совпадать с каноном
	// /seo (canonicalChapter) буква в букву — по ним части лежат в кэше.
	ch, err := s.Chapters.GetByID(ctx, chapterID)
	if err != nil {
		return "", nil, err
	}
	w, err := s.Works.GetByID(ctx, workID)
	if err != nil {
		return "", nil, err
	}
	chapter := chapterPath(workID, w.Slug, chapterID, ch.Slug)

	text, starts := book.MarkdownWithPageStarts(b)
	parts := splitParts(text, starts, llmPartRunes)
	docs := make([]*TextDoc, len(parts))
	for i := range parts {
		docs[i] = s.chapterPartDoc(b, chapter, parts, i+1)
	}
	return chapter, docs, nil
}

// chapterPartDoc — часть part (с единицы) из разрезанного текста главы:
// шапка, навигация по частям, тело.
func (s *Source) chapterPartDoc(b *book.Book, chapter string, parts []textPart, part int) *TextDoc {
	p := parts[part-1]

	var out strings.Builder
	title := "Глава «" + OneLine(b.Meta.Title) + "»"
	if a := OneLine(strings.Join(b.Meta.Authors, ", ")); a != "" {
		title += " — " + a
	}
	out.WriteString(title + "\n")
	if src := book.SourceLine(b.Meta); src != "" {
		out.WriteString("Источник: " + OneLine(src) + "\n")
	}
	out.WriteString("Адрес главы: " + s.abs(chapter) + "\n")
	if len(parts) > 1 {
		fmt.Fprintf(&out, "Часть %d из %d, с. %d—%d.", part, len(parts), p.FromPrinted, p.ToPrinted)
		if part > 1 {
			out.WriteString(" Предыдущая: " + s.abs(chapterTextPath(chapter, part-1)) + ".")
		}
		if part < len(parts) {
			out.WriteString(" Следующая: " + s.abs(chapterTextPath(chapter, part+1)) + ".")
		}
		out.WriteString("\n")
	}
	out.WriteString("\n" + LLMReadingNote + "\n")
	if !bookHasText(b) {
		out.WriteString("\nТекста на этих страницах нет.\n")
	}
	out.WriteString("\n---\n\n")
	out.WriteString(p.Body)
	// Последней строкой, а не только в шапке: модель, дочитавшая до конца,
	// не возвращается к началу.
	if part < len(parts) {
		out.WriteString("Продолжение: " + s.abs(chapterTextPath(chapter, part+1)) + "\n")
	}

	return &TextDoc{
		Body:      out.String(),
		Canonical: s.abs(chapter),
		NoIndex:   true,
		CacheKey:  b.Meta.CacheKey,
	}
}

// bookHasText — есть ли в книге хоть одна полоса с текстом.
func bookHasText(b *book.Book) bool {
	for _, s := range b.Sections {
		if sectionHasText(s) {
			return true
		}
	}
	return false
}

func sectionHasText(s book.Section) bool {
	for _, bl := range s.Blocks {
		if bl.Child != nil {
			if sectionHasText(*bl.Child) {
				return true
			}
			continue
		}
		for _, p := range bl.Pages {
			if strings.TrimSpace(p.Markdown) != "" {
				return true
			}
		}
	}
	return false
}

// mdLinkText экранирует текст ссылки markdown: заглавие со скобками иначе
// рвёт ссылку.
var mdLinkEscaper = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`)

func mdLinkText(s string) string { return mdLinkEscaper.Replace(s) }

// WorkText — оглавление тома для нейросети: дерево глав ссылками на их
// тексты. Текста полос не читает — лёгкий запрос, без ограничителя.
func (s *Source) WorkText(ctx context.Context, id int64) (*TextDoc, error) {
	work, err := s.Works.GetByID(ctx, id)
	if isNotFound(err) {
		return nil, notFound("работа %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("работа %d: %w", id, err)
	}
	if work == nil {
		return nil, notFound("работа %d", id)
	}
	// Как у Work(): передние листы под своим адресом — дубль, достижимы из тома.
	if work.Role == models.WorkRoleFrontMatter {
		return nil, notFound("работа %d служебная", id)
	}
	chapters, err := s.Chapters.ListByWorkHierarchical(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("главы работы %d: %w", id, err)
	}
	children, err := s.Works.ListChildren(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("передние листы работы %d: %w", id, err)
	}

	var out strings.Builder
	out.WriteString("# " + OneLine(work.Title) + "\n\n")
	if a := OneLine(work.Author); a != "" {
		out.WriteString(a + "\n")
	}
	if ed := OneLine(s.editionTitleOf(ctx, work)); ed != "" {
		out.WriteString(ed + "\n")
	}
	out.WriteString("Адрес тома: " + s.abs(workPath(work.ID, work.Slug)) + "\n\n")
	out.WriteString("> Оглавление тома. У каждой главы — адрес её текста в markdown;\n" +
		"> номера страниц — печатного издания.\n\n")

	out.WriteString("## Содержание\n\n")
	s.writeChapterTextList(&out, chapters, work, 0, true)

	var front strings.Builder
	for _, child := range children {
		if child.Role != models.WorkRoleFrontMatter {
			continue
		}
		list, err := s.Chapters.ListByWorkHierarchical(ctx, child.ID)
		if err != nil {
			return nil, fmt.Errorf("главы передних листов %d: %w", child.ID, err)
		}
		if len(list) == 0 {
			continue
		}
		front.WriteString("### " + OneLine(child.Title) + "\n\n")
		// Номеров нет: у передних листов своя, римская нумерация.
		s.writeChapterTextList(&front, list, child, 0, false)
		front.WriteString("\n")
	}
	if front.Len() > 0 {
		out.WriteString("\n## Предваряющие материалы\n\n" + front.String())
	}

	return &TextDoc{
		Body:      out.String(),
		Canonical: s.abs(workPath(work.ID, work.Slug)),
		NoIndex:   true,
		CacheKey:  work.UpdatedAt,
	}, nil
}

// writeChapterTextList — дерево глав списком с отступом в два пробела на
// уровень. pages — печатать ли печатные номера (внутренние + page_offset).
func (s *Source) writeChapterTextList(out *strings.Builder, chapters []*models.Chapter, w *models.Work, depth int, pages bool) {
	for _, c := range chapters {
		fmt.Fprintf(out, "%s- [%s](%s)", strings.Repeat("  ", depth),
			mdLinkText(OneLine(c.Title)),
			s.abs(chapterTextPath(chapterPath(w.ID, w.Slug, c.ID, c.Slug), 1)))
		if pages {
			fmt.Fprintf(out, " — с. %d—%d", c.StartPage+w.PageOffset, c.EndPage+w.PageOffset)
		}
		if c.IsApparatus {
			out.WriteString(" (аппарат издания)")
		}
		out.WriteString("\n")
		s.writeChapterTextList(out, c.Children, w, depth+1, pages)
	}
}

// LLMsText — /llms.txt по соглашению llmstxt.org: что это за библиотека, как
// устроены адреса текстов и все тома ссылками на их оглавления.
func (s *Source) LLMsText(ctx context.Context) (*TextDoc, error) {
	editions, err := s.Editions.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("издания: %w", err)
	}
	loose, err := s.Works.ListWithoutEdition(ctx)
	if err != nil {
		return nil, fmt.Errorf("тома вне изданий: %w", err)
	}
	base := s.abs("")

	var out strings.Builder
	out.WriteString("# " + site.Name() + "\n\n")
	out.WriteString("> " + site.Description() + " " +
		"Любой текст отсюда можно дать нейросети ссылкой и спрашивать о нём, " +
		"получая ответы со ссылками на страницы печатного издания.\n\n")
	out.WriteString("## Как читать тексты читальни\n\n")
	out.WriteString("- Глава: " + base + "/works/{том}/chapters/{глава}.md — текст главы в markdown. " +
		"Длинная глава приходит частями: адрес следующей части стоит в шапке и последней строкой («Продолжение: …»).\n")
	out.WriteString("- Том: " + base + "/works/{том}.md — оглавление тома со ссылками на тексты глав.\n")
	out.WriteString("- Понятие предметного указателя: " + base + "/concepts/{слаг}.md — статья указателя, " +
		"подрубрики и текст всех мест в томах, частями, как глава.\n")
	out.WriteString("- Все понятия указателя: " + base + "/concepts.md — списком со ссылками на их тексты.\n")
	out.WriteString("- Вопросы по всей читальне: MCP-сервер " + base + "/mcp — подключается к Claude и ChatGPT " +
		"как коннектор, ищет по всем томам и открывает страницы.\n")
	// Обычная страница главы — HTML, который nginx отдаёт готовым только
	// сборщикам из карты $is_crawler (ChatGPT-User, Claude-User,
	// Perplexity-User); остальные получат пустую оболочку SPA. Поэтому и
	// сказано, кому она открывается, а не «тоже подойдёт».
	out.WriteString("- Обычная ссылка на главу (" + base + "/works/{том}/chapters/{глава}) " +
		"открывается сборщикам ChatGPT, Claude и Perplexity, но длинную главу чат может обрезать; " +
		".md открывается любому чату и надёжнее.\n")
	// Строка начинается не с «- [»: так начинаются только ссылки на тома.
	out.WriteString("- Номер в квадратных скобках, [123], — страница печатного издания, ссылайтесь на него. " +
		"Сноски — [^123-1], их текст на той же странице.\n\n")

	out.WriteString("## Издания\n\n")
	for _, ed := range editions {
		works, err := s.Editions.ListWorks(ctx, ed.ID)
		if err != nil {
			return nil, fmt.Errorf("тома издания %d: %w", ed.ID, err)
		}
		out.WriteString("### " + OneLine(ed.Title) + "\n\n")
		for _, w := range works {
			// Как у Edition(): передние листы достижимы из своего тома.
			if w.Role == models.WorkRoleFrontMatter {
				continue
			}
			fmt.Fprintf(&out, "- [%s](%s)\n", mdLinkText(OneLine(w.Title)), s.abs(workPath(w.ID, w.Slug)+".md"))
		}
		out.WriteString("\n")
	}
	if len(loose) > 0 {
		out.WriteString("## Отдельные книги\n\n")
		for _, w := range loose {
			fmt.Fprintf(&out, "- [%s](%s)\n", mdLinkText(OneLine(w.Title)), s.abs(workPath(w.ID, w.Slug)+".md"))
		}
	}
	return &TextDoc{Body: out.String()}, nil
}
