package seo

import (
	"context"
	"fmt"
	"net/url"
	"proofreader/internal/site"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"proofreader/pkg/book"
)

// Понятие указателя для нейросетей: /concepts/{слаг}.md и части. Спека —
// docs/superpowers/specs/2026-10-02-concept-for-llm-design.md. Книгу собирает
// internal/api (ConceptBookSource), здесь — шапка, оглавление и разрез тем же
// splitParts, что у главы.

// conceptReadingNote — как читать текст понятия. Своя, не LLMReadingNote: в
// главе том один, здесь их много, и номер страницы без тома ничего не значит.
const conceptReadingNote = "> Как читать: заголовок места — том и страницы печатного издания, он же ссылка на место в читальне.\n" +
	"> [123] перед абзацами — номер страницы этого тома, ссылайтесь на него вместе с томом.\n" +
	"> Сноски помечены [^123-…], их текст — на той же странице. Страница, которая уже приведена выше,\n" +
	"> второй раз не печатается — на её месте ссылка на место, где она есть."

func conceptBase(slug string) string { return "/concepts/" + slug }

// ParseRubricPath — значение ?rubric_path= страницы понятия в путь подрубрики:
// звенья через «:», каждое закодировано отдельно (encodeRubricPath в
// frontend/src/utils/rubricPathParam.ts). Звено, которое не раскодировалось
// или пусто, роняет весь путь — nil: путь с выпавшим звеном — другой путь.
// Общий для потока понятия (internal/api) и его .md.
func ParseRubricPath(raw string) []string {
	segs := strings.Split(raw, ":")
	path := make([]string, 0, len(segs))
	for _, seg := range segs {
		decoded, err := url.PathUnescape(seg)
		if err != nil || decoded == "" || !utf8.ValidString(decoded) {
			return nil
		}
		path = append(path, decoded)
	}
	return path
}

// RubricQuery — rubricQuery для MCP-сервера: его fetch просит текст
// подрубрики тем же путём, что и краулер, и ключ обязан совпасть.
func RubricQuery(rubric []string) string { return rubricQuery(rubric) }

// rubricQuery — канонический хвост адреса текста подрубрики: «?rubric_path=…»
// или пусто без подрубрики. Им же ключуются части в кэше и строятся ссылки
// на части, поэтому вид у него один на все пути, как бы его ни закодировал
// пришедший: Page разбирает пришедшее значение и пересобирает его этой
// функцией. Звенья — QueryEscape с пробелом %20 (двоеточие внутри звена
// кодируется и с разделителем не спутается), значение целиком — ещё раз.
//
// С формой страницы понятия (encodeRubricPath + URLSearchParams) строка
// совпадает буква в букву для кириллицы, пробела, тире и «-_.~», но не для
// «! ' ( ) *»: encodeURIComponent их не кодирует, QueryEscape кодирует. Это
// безопасно — оба вида разбираются в один путь и ведут к одной записи, — но
// адрес кнопки и канон сервера у таких подрубрик различаются написанием.
func rubricQuery(rubric []string) string {
	if len(rubric) == 0 {
		return ""
	}
	segs := make([]string, len(rubric))
	for i, r := range rubric {
		segs[i] = strings.ReplaceAll(url.QueryEscape(r), "+", "%20")
	}
	return "?rubric_path=" + url.QueryEscape(strings.Join(segs, ":"))
}

// outlineSlack — запас к длине оглавления между двумя разрезами: номер части
// у подрубрики может сменить разрядность («часть 9» → «часть 10»).
const outlineSlack = 2000

// ConceptTextParts — все части текста понятия разом (как ChapterTextParts):
// часть стоит сборки всего понятия. rubric — подрубрика (?rubric_path=) или
// nil; адреса частей несут её хвост (rubricQuery).
func (s *Source) ConceptTextParts(ctx context.Context, slug string, rubric []string) (string, []*TextDoc, error) {
	cb, err := s.ConceptBooks.Concept(ctx, slug, rubric)
	if err != nil {
		return "", nil, err
	}
	base := conceptBase(slug)
	text, starts := book.MarkdownWithPageStarts(cb.Book)
	// Два разреза: первый — чтобы узнать длину оглавления, второй — с
	// пределом части 1, уменьшенным на неё. Запас — на смену разрядности
	// номеров частей между разрезами.
	parts := splitParts(text, starts, llmPartRunes)
	toc := conceptOutline(cb, pageParts(parts, starts), len(parts) > 1)
	first := max(llmPartRunes-utf8.RuneCountInString(toc)-outlineSlack, llmPartRunes/4)
	parts = splitPartsFirst(text, starts, first, llmPartRunes)
	toc = conceptOutline(cb, pageParts(parts, starts), len(parts) > 1)

	docs := make([]*TextDoc, len(parts))
	for i := range parts {
		docs[i] = s.conceptPartDoc(cb, base, rubric, parts, i+1, toc)
	}
	return base, docs, nil
}

// pageParts — номер части (с единицы) каждой полосы книги. Разрез идёт ровно
// по началам полос, поэтому начало, совпавшее с границей, — уже следующая
// часть.
func pageParts(parts []textPart, starts []book.PageStart) []int {
	out := make([]int, len(starts))
	k, end := 0, len(parts[0].Body)
	for i, st := range starts {
		for st.Offset >= end && k+1 < len(parts) {
			k++
			end += len(parts[k].Body)
		}
		out[i] = k + 1
	}
	return out
}

func (s *Source) conceptPartDoc(cb *ConceptBook, base string, rubric []string, parts []textPart, part int, toc string) *TextDoc {
	c := cb.Concept
	q := rubricQuery(rubric)
	partURL := func(k int) string { return s.abs(chapterTextPath(base, k) + q) }
	var out strings.Builder
	out.WriteString("Понятие «" + OneLine(c.Title) + "» — предметный указатель\n")
	if eds := conceptEditions(cb); eds != "" {
		out.WriteString("Источник: " + eds + "\n")
	}
	out.WriteString("Адрес понятия: " + s.abs(base) + "\n")
	if len(rubric) > 0 {
		// Модель обязана знать, что перед ней не всё понятие, и где взять всё.
		names := make([]string, len(rubric))
		for i, r := range rubric {
			names[i] = "«" + OneLine(r) + "»"
		}
		out.WriteString("Только подрубрика " + strings.Join(names, " / ") +
			". Понятие целиком: " + s.abs(chapterTextPath(base, 1)) + "\n")
	}
	if len(parts) > 1 {
		fmt.Fprintf(&out, "Часть %d из %d.", part, len(parts))
		if part > 1 {
			out.WriteString(" Предыдущая: " + partURL(part-1) + ".")
		}
		if part < len(parts) {
			out.WriteString(" Следующая: " + partURL(part+1) + ".")
		}
		out.WriteString("\n")
	}
	out.WriteString("\n" + conceptReadingNote + "\n")
	if part == 1 {
		out.WriteString("\n" + toc)
	}
	out.WriteString("\n---\n\n")
	out.WriteString(parts[part-1].Body)
	if part < len(parts) {
		out.WriteString("Продолжение: " + partURL(part+1) + "\n")
	}
	return &TextDoc{
		Body:      out.String(),
		Canonical: s.abs(base) + q,
		NoIndex:   true,
		CacheKey:  cb.Modified,
	}
}

// conceptEditions — издания статей через «; », без повторов. Статья без
// адресов и без текста (пустая отсылка другого указателя) издания не даёт.
func conceptEditions(cb *ConceptBook) string {
	var eds []string
	for _, a := range cb.Concept.Articles {
		if len(a.References) == 0 && strings.TrimSpace(a.ArticleMarkdown) == "" {
			continue
		}
		if e := OneLine(a.EditionTitle); e != "" && !containsString(eds, e) {
			eds = append(eds, e)
		}
	}
	return strings.Join(eds, "; ")
}

// conceptOutline — статья указателя (если её текст сохранился), сводка и
// дерево подрубрик. withParts — печатать ли номер части.
func conceptOutline(cb *ConceptBook, partOf []int, withParts bool) string {
	var out strings.Builder
	var articles []string
	for _, a := range cb.Concept.Articles {
		if strings.TrimSpace(a.ArticleMarkdown) != "" {
			articles = append(articles, a.ArticleMarkdown)
		}
	}
	if len(articles) > 0 {
		out.WriteString("## Статья указателя\n\n")
		for _, a := range cb.Concept.Articles {
			if strings.TrimSpace(a.ArticleMarkdown) == "" {
				continue
			}
			if len(articles) > 1 {
				out.WriteString("### " + OneLine(a.EditionTitle) + "\n\n")
			}
			out.WriteString(strings.TrimSpace(a.ArticleMarkdown) + "\n\n")
		}
	}
	if cb.Places == 0 {
		return out.String()
	}
	out.WriteString("## Оглавление понятия\n\n")
	fmt.Fprintf(&out, "Мест в указателе: %d; в читальне: %d, на %d страницах.\n\n", cb.Places, cb.Present, cb.Pages)
	// Заголовок издания — только когда подрубрики из нескольких изданий.
	editions := map[string]bool{}
	for _, r := range cb.Rubrics {
		editions[r.Edition] = true
	}
	edition := ""
	for i, r := range cb.Rubrics {
		if len(editions) > 1 && r.Edition != edition {
			edition = r.Edition
			if i > 0 {
				out.WriteString("\n")
			}
			out.WriteString("### " + OneLine(edition) + "\n\n")
		}
		// Пустой Path — адреса без подрубрики: нулевой уровень и своя подпись.
		depth, label := 0, "без подрубрики"
		if len(r.Path) > 0 {
			depth, label = len(r.Path)-1, OneLine(r.Path[len(r.Path)-1])
		}
		fmt.Fprintf(&out, "%s- %s (мест: %d; %s", strings.Repeat("  ", depth),
			label, r.Places, volumeList(r.Volumes))
		switch {
		case r.FirstPage < 0:
			out.WriteString("; в читальне нет")
		case withParts:
			fmt.Fprintf(&out, "; часть %d", partOf[r.FirstPage])
		}
		out.WriteString(")\n")
	}
	return out.String()
}

// volumeList — «т. 23» или «тт. 12—13, 16, 23»: подряд идущие номера
// схлопываются, иначе у ленинской подрубрики на 55 томов строка оглавления
// вышла бы длиннее самой подрубрики.
func volumeList(vols []int) string {
	vs := append([]int(nil), vols...)
	sort.Ints(vs)
	var spans []string
	for i := 0; i < len(vs); {
		j := i
		for j+1 < len(vs) && vs[j+1] == vs[j]+1 {
			j++
		}
		if j > i {
			spans = append(spans, fmt.Sprintf("%d—%d", vs[i], vs[j]))
		} else {
			spans = append(spans, fmt.Sprint(vs[i]))
		}
		i = j + 1
	}
	if len(vs) == 1 {
		return "т. " + spans[0]
	}
	return "тт. " + strings.Join(spans, ", ")
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// ConceptShelfTextParts — витрина понятий для нейросетей: все понятия-статьи
// по буквам, со ссылкой на текст каждого и числом мест.
func (s *Source) ConceptShelfTextParts(ctx context.Context) (string, []*TextDoc, error) {
	rows, err := s.Catalog.ConceptShelf(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("витрина понятий: %w", err)
	}
	const base = "/concepts"
	// Порядок — по ключу без кавычек, а не тот, что дал SQL: иначе буква,
	// встреченная в кавычках раньше, разбилась бы на две группы.
	sort.SliceStable(rows, func(i, j int) bool {
		return shelfKey(rows[i].SortKey) < shelfKey(rows[j].SortKey)
	})
	var body strings.Builder
	var starts []book.PageStart
	letter := ""
	for _, r := range rows {
		if l := shelfLetter(r.SortKey); l != letter {
			if letter != "" {
				body.WriteString("\n")
			}
			letter = l
			starts = append(starts, book.PageStart{Offset: body.Len()})
			body.WriteString("## " + l + "\n\n")
		}
		fmt.Fprintf(&body, "- [%s](%s) — мест: %d\n", mdLinkText(OneLine(r.Title)),
			s.abs(chapterTextPath(conceptBase(r.Slug), 1)), r.Places)
	}
	parts := splitParts(body.String(), starts, llmPartRunes)
	docs := make([]*TextDoc, len(parts))
	for i := range parts {
		docs[i] = s.shelfPartDoc(base, parts, i+1)
	}
	return base, docs, nil
}

// shelfKey — ключ сортировки без ведущих небуквенных знаков: у 113 понятий
// корпуса он начинается с «««» («Борьба», «Arbeiterpolitik»), и по сырому
// ключу они встали бы отдельной группой «» в начале витрины.
func shelfKey(sortKey string) string {
	return strings.TrimLeftFunc(sortKey, func(r rune) bool { return !unicode.IsLetter(r) })
}

// shelfLetter — буква витрины: первая буква ключа, заглавная.
func shelfLetter(sortKey string) string {
	for _, r := range shelfKey(sortKey) {
		return string(unicode.ToUpper(r))
	}
	return "#"
}

func (s *Source) shelfPartDoc(base string, parts []textPart, part int) *TextDoc {
	var out strings.Builder
	out.WriteString("# Предметный указатель — " + site.Name() + "\n\n")
	out.WriteString("Адрес указателя: " + s.abs(base) + "\n")
	if len(parts) > 1 {
		fmt.Fprintf(&out, "Часть %d из %d.", part, len(parts))
		if part > 1 {
			out.WriteString(" Предыдущая: " + s.abs(chapterTextPath(base, part-1)) + ".")
		}
		if part < len(parts) {
			out.WriteString(" Следующая: " + s.abs(chapterTextPath(base, part+1)) + ".")
		}
		out.WriteString("\n")
	}
	out.WriteString("\n> Все понятия предметных указателей читальни. По ссылке у понятия — его текст:\n" +
		"> статья указателя, подрубрики и все места в томах, страницы печатного издания.\n\n---\n\n")
	out.WriteString(parts[part-1].Body)
	if part < len(parts) {
		out.WriteString("Продолжение: " + s.abs(chapterTextPath(base, part+1)) + "\n")
	}
	return &TextDoc{Body: out.String(), Canonical: s.abs(base), NoIndex: true}
}
