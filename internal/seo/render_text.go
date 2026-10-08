package seo

import (
	"context"
	"fmt"
	"proofreader/internal/site"
	"strings"

	"proofreader/pkg/book"
)

// descriptionRunes — длина описания страницы. 250 знаков: Гугл показывает
// около 160, Телеграм — около 200, запас нужен на обрезку по границе слова.
const descriptionRunes = 250

// Chapter — единственная страница с полным текстом. Тело берётся из
// book.BodyHTML: та же функция печатает скачиваемый файл, и держать два
// представления одного текста нельзя — поисковик проиндексировал бы одно, а
// читатель скачал другое.
func (s *Source) Chapter(ctx context.Context, workID, chapterID int64) (*Doc, error) {
	// Ошибку не переворачиваем: ErrNotFound уже переведён адаптером
	// (api.SEOBookSource, задача 10) — различать 404 от поломки его забота.
	b, err := s.Books.Chapter(ctx, workID, chapterID)
	if err != nil {
		return nil, err
	}

	// Слаги главы и её тома — из b.Meta их не взять: там адрес тома
	// (Meta.URL, pkg/book/book.go), а не главы, — и они печатаются в
	// скачиваемый файл как источник (pkg/book/epub.go, fb2.go). Два лишних
	// чтения строки на пути, который рендерит до 742 полос, — шум; на пути
	// 301 (задача 10) рендера не будет вовсе.
	ch, err := s.Chapters.GetByID(ctx, chapterID)
	if err != nil {
		return nil, err
	}
	w, err := s.Works.GetByID(ctx, workID)
	if err != nil {
		return nil, err
	}
	canonical := chapterPath(workID, w.Slug, chapterID, ch.Slug)
	// Текст для нейросетей: длинную главу чат по этой странице обрежет, а
	// строку в самом начале увидит и пойдёт по частям.
	alternate := s.abs(chapterTextPath(canonical, 1))

	title := OneLine(b.Meta.Title)
	author := strings.Join(b.Meta.Authors, ", ")

	// Различающее — вперёд, автор — следом: та же причина, что в
	// render_volume.go/Work — вкладку и сниппет обрезают с конца, а корпус
	// собирает десятки глав одного автора под общими заголовками.
	fullTitle := title
	if author != "" {
		fullTitle = title + " — " + author
	}

	// Пустой ключ "author" — ошибка проверки в Яндекс.Вебмастере: для главы
	// без автора ключ не печатаем вовсе, а не оставляем пустой строкой.
	jsonLD := map[string]any{
		"@context":     "https://schema.org",
		"@type":        "Article",
		"headline":     title,
		"inLanguage":   "ru",
		"isPartOf":     sentences(OneLine(b.Meta.Edition), OneLine(b.Meta.Volume)),
		"url":          s.abs(canonical),
		"dateModified": b.Meta.Modified.UTC().Format("2006-01-02"),
	}
	if author != "" {
		jsonLD["author"] = author
	}

	return &Doc{
		Title: fullTitle + " — " + site.Name(),
		Description: sentences(
			Excerpt(firstText(b, descriptionRunes), descriptionRunes),
		),
		Canonical: s.abs(canonical),
		Alternate: alternate,
		OGType:    "article",
		ImageURL:  s.abs(fmt.Sprintf("/og/chapter/%d.png", chapterID)),
		Robots:    RobotsIndex,
		JSONLD:    jsonLD,
		Body:      llmNote(alternate) + book.BodyHTML(b),
		CacheKey:  b.Meta.CacheKey,
	}, nil
}

// firstText копит начало текста книги для описания. Титульный блок и
// оглавление не годятся: там выходные данные, а под ссылкой в выдаче читатель
// ждёт текст.
//
// Предел в байтах, а не в знаках: разметка раздувает HTML, а пересчитывать
// руны на каждой странице значило бы обходить накопленное заново.
func firstText(b *book.Book, minRunes int) string {
	var out strings.Builder
	enough := minRunes * 8

	var walk func(s book.Section)
	walk = func(s book.Section) {
		if out.Len() >= enough {
			return
		}
		for _, blk := range s.Blocks {
			if out.Len() >= enough {
				return
			}
			if blk.Child != nil {
				walk(*blk.Child)
				continue
			}
			for _, p := range blk.Pages {
				out.WriteString(p.HTML)
				out.WriteString(" ")
				if out.Len() >= enough {
					return
				}
			}
		}
	}
	for _, sec := range b.Sections {
		walk(sec)
	}
	return out.String()
}

// ReadPage — одна полоса. Теги превью у неё есть: читатель делится в
// мессенджере тем, что читает сейчас, а краулер мессенджера robots не
// смотрит. В индекс полоса не идёт — это тысячи почти одинаковых адресов,
// дробящих один текст, — поэтому canonical ведёт на главу.
func (s *Source) ReadPage(ctx context.Context, workID int64, pageNumber int) (*Doc, error) {
	work, err := s.Works.GetByID(ctx, workID)
	if isNotFound(err) {
		return nil, notFound("работа %d", workID)
	}
	if err != nil {
		return nil, fmt.Errorf("работа %d: %w", workID, err)
	}
	if work == nil {
		return nil, notFound("работа %d", workID)
	}

	page, err := s.Pages.GetByWorkAndPageNumber(ctx, workID, pageNumber)
	if isNotFound(err) {
		return nil, notFound("полоса %d работы %d", pageNumber, workID)
	}
	if err != nil {
		return nil, fmt.Errorf("полоса %d работы %d: %w", pageNumber, workID, err)
	}
	if page == nil {
		return nil, notFound("полоса %d работы %d", pageNumber, workID)
	}

	// Канонический адрес — глава, накрывающая полосу, иначе карточка тома:
	// адрес полосы каноническим быть не может, он же noindex. Сам адрес при
	// этом не меняется (глава или том), но обязан нести слаг.
	//
	// Глава ищется по диапазону здесь, а не читается из page.ChapterID:
	// поле есть в схеме, но конвейер не заполняет его ни одной полосе
	// корпуса, и ветка на нём была мертва целиком — боевой отдавал том на
	// каждой полосе, тогда как на карточке тома текста цитируемой полосы нет,
	// там список глав. Заполнить поле нельзя: конвейер перестраивает главы
	// при каждом прогоне тома (оттого и ON DELETE SET NULL), хранимая ссылка
	// поехала бы вразнос. Цена — один запрос по idx_chapters_work на полосу,
	// та же выборка, что уже держит выдачу поиска.
	canonical := s.abs(workPath(workID, work.Slug))
	ch, err := s.Chapters.FindByPage(ctx, workID, pageNumber)
	if err != nil {
		return nil, fmt.Errorf("глава полосы %d работы %d: %w", pageNumber, workID, err)
	}
	if ch != nil {
		canonical = s.abs(chapterPath(workID, work.Slug, ch.ID, ch.Slug))
	}

	// Со сносками — разметкой главы (RenderWithNotes), а не <ol> gomarkdown,
	// который нумерует пункты подряд и печатал примечание тома единицей.
	//
	// Renderer.Render отдаёт XHTML-фрагмент: обёртку документа снимает сам
	// pkg/markdown (см. контракт в докблоке пакета). Раньше её снимали здесь
	// вручную, и ошибка разбора отдавала 500 — намеренно, а не по
	// небрежности: пустая проиндексированная страница хуже, чем повторный
	// визит краулера позже. Развилка уехала внутрь пакета, где ветка ошибки
	// недостижима (источник разбора — строка), поэтому выбора здесь больше
	// нет; если xhtml.Body однажды начнёт получать не строку, решение
	// придётся принимать заново.
	pageHTML := s.Renderer.RenderWithNotes("", pageNumber, page.ContentMarkdown)
	printed := pageNumber + work.PageOffset

	var body strings.Builder
	fmt.Fprintf(&body, "<h1>%s, с. %d</h1>\n", Esc(OneLine(work.Title)), printed)
	body.WriteString(pageHTML)

	return &Doc{
		Title:       fmt.Sprintf("%s, с. %d — %s", OneLine(work.Title), printed, site.Name()),
		Description: Excerpt(pageHTML, descriptionRunes),
		Canonical:   canonical,
		OGType:      "article",
		ImageURL:    s.abs(fmt.Sprintf("/og/work/%d.png", workID)),
		Robots:      RobotsNoIndex,
		Body:        body.String(),
		CacheKey:    page.UpdatedAt,
	}, nil
}

func llmNote(href string) string {
	return `<p class="llm-note">Текст для нейросетей, по частям: <a href="` + Esc(href) + `">` +
		Esc(href) + "</a></p>\n"
}
