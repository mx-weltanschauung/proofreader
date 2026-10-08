package seo

import (
	"context"
	"fmt"
	"log"
	"proofreader/internal/site"
	"strings"

	"proofreader/internal/models"
)

// Work — карточка тома: выходные данные и дерево глав ссылками. Текста
// страниц здесь нет намеренно (см. спеку, раздел «Что в теле»): том Ленина
// целиком — мегабайты, а рядом лежат адреса глав, по которым краулер и
// доберётся до текста.
func (s *Source) Work(ctx context.Context, id int64) (*Doc, error) {
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
	// Служебные передние листы скрыты из каталога и достижимы только с
	// карточки родителя. Под своим адресом это был бы дубль текста тома.
	if work.Role == models.WorkRoleFrontMatter {
		return nil, notFound("работа %d служебная", id)
	}

	chapters, err := s.Chapters.ListByWorkHierarchical(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("главы работы %d: %w", id, err)
	}

	editionTitle := s.editionTitleOf(ctx, work)

	var body strings.Builder
	body.WriteString("<h1>" + Esc(OneLine(work.Title)) + "</h1>\n")
	if work.Author != "" {
		body.WriteString("<p>" + Esc(OneLine(work.Author)) + "</p>\n")
	}
	if editionTitle != "" {
		body.WriteString("<p>" + Esc(OneLine(editionTitle)) + "</p>\n")
	}
	writeChapterList(&body, chapters, id, work.Slug)

	// Различающее — вперёд, автор — следом. У этого корпуса 45 томов одного
	// автора: поставь автора первым, и вкладки браузера (обрезает конец) и
	// сниппет поисковика (тоже режет примерно на 60 знаках) одинаково начинались
	// бы с «В. И. Ленин.» — закладка на том снова стала бы неотличима от
	// закладки на главу, то есть та беда, ради которой заведён этот заголовок.
	title := OneLine(work.Title)
	if work.Author != "" {
		title = title + " — " + OneLine(work.Author)
	}

	// Пустой ключ "author" — ошибка проверки в Яндекс.Вебмастере, а не
	// нейтральное «нет данных»: для тома без автора (подборка, коллективный
	// труд) ключ не печатаем вовсе, а не оставляем пустой строкой.
	jsonLD := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "Book",
		"name":       OneLine(work.Title),
		"inLanguage": "ru",
		"url":        s.abs(workPath(id, work.Slug)),
	}
	if a := OneLine(work.Author); a != "" {
		jsonLD["author"] = a
	}

	return &Doc{
		Title: title + " — " + site.Name(),
		Description: sentences(
			OneLine(work.Title),
			OneLine(work.Author),
			OneLine(editionTitle),
			// Не «%d глав»: русское счётное слово меняет форму по числу
			// (33 главы, 5 глав, 21 глава), а отдельный помощник склонения
			// ради одной строки — оверинжиниринг. Двоеточие снимает
			// вопрос — тем же приёмом, что уже стоит на карточках (card.go:
			// «Томов в читальне: %d», «Адресов в корпусе: %d»).
			fmt.Sprintf("Глав: %d", countChapters(chapters)),
		),
		Canonical: s.abs(workPath(id, work.Slug)),
		OGType:    "book",
		ImageURL:  s.abs(fmt.Sprintf("/og/work/%d.png", id)),
		Robots:    RobotsIndex,
		JSONLD:    jsonLD,
		Body:      body.String(),
		CacheKey:  work.UpdatedAt,
	}, nil
}

// writeChapterList печатает дерево глав ссылками, на любую глубину: краулер
// идёт по ссылкам, и вложенная глава без ссылки для него не существует.
// Ссылки ведут на канон (со слагом тома и главы), иначе каждый переход по
// оглавлению — лишний 301.
func writeChapterList(out *strings.Builder, chapters []*models.Chapter, workID int64, workSlug string) {
	if len(chapters) == 0 {
		return
	}
	out.WriteString(`<nav class="toc"><h2>Содержание</h2>` + "\n<ul>\n")
	for _, c := range chapters {
		fmt.Fprintf(out, `<li><a href="%s">%s</a>`,
			chapterPath(workID, workSlug, c.ID, c.Slug), Esc(OneLine(c.Title)))
		writeChapterList(out, c.Children, workID, workSlug)
		out.WriteString("</li>\n")
	}
	out.WriteString("</ul>\n</nav>\n")
}

func countChapters(chapters []*models.Chapter) int {
	n := 0
	for _, c := range chapters {
		n += 1 + countChapters(c.Children)
	}
	return n
}

// Edition — страница издания: описание и тома ссылками.
func (s *Source) Edition(ctx context.Context, id int64) (*Doc, error) {
	ed, err := s.Editions.GetByID(ctx, id)
	if isNotFound(err) {
		return nil, notFound("издание %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("издание %d: %w", id, err)
	}
	if ed == nil {
		return nil, notFound("издание %d", id)
	}

	works, err := s.Editions.ListWorks(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("тома издания %d: %w", id, err)
	}

	edSlug := effectiveEditionSlug(ed)

	var body strings.Builder
	body.WriteString("<h1>" + Esc(OneLine(ed.Title)) + "</h1>\n")
	if ed.Description != "" {
		body.WriteString("<p>" + Esc(OneLine(ed.Description)) + "</p>\n")
	}
	shown := 0
	body.WriteString("<ul>\n")
	for _, w := range works {
		if w.Role == models.WorkRoleFrontMatter {
			continue
		}
		fmt.Fprintf(&body, `<li><a href="%s">%s</a></li>`+"\n", workPath(w.ID, w.Slug), Esc(OneLine(w.Title)))
		shown++
	}
	body.WriteString("</ul>\n")

	return &Doc{
		Title: OneLine(ed.Title) + " — " + site.Name(),
		Description: sentences(OneLine(ed.Title), OneLine(ed.Description),
			// Та же правка счётной формы, что и у глав выше, и тем же
			// приёмом, что уже стоит на карточке издания (card.go:407).
			fmt.Sprintf("Томов в читальне: %d", shown)),
		Canonical: s.abs(editionPath(id, edSlug)),
		OGType:    "website",
		ImageURL:  s.abs(fmt.Sprintf("/og/edition/%d.png", id)),
		Robots:    RobotsIndex,
		JSONLD: map[string]any{
			"@context": "https://schema.org",
			"@type":    "CollectionPage",
			"name":     OneLine(ed.Title),
			"url":      s.abs(editionPath(id, edSlug)),
		},
		Body:     body.String(),
		CacheKey: ed.UpdatedAt,
	}, nil
}

// editionTitleOf — название издания тома или пусто. Удалённое издание —
// молча пусто; отказ базы — в журнал и тоже пусто: карточка тома без строки
// издания лучше, чем 500, но молчанием поломку прикрывать нельзя.
func (s *Source) editionTitleOf(ctx context.Context, work *models.Work) string {
	if work.EditionID == nil {
		return ""
	}
	ed, err := s.Editions.GetByID(ctx, *work.EditionID)
	switch {
	case isNotFound(err):
		// Издание удалено — строки с изданием просто не будет. Молчим осознанно.
	case err != nil:
		// Без записи больная база отдаёт краулеру урезанную карточку, он её
		// индексирует, и сигнала об этом нет нигде.
		log.Printf("работа %d: не удалось получить издание %d: %v", work.ID, *work.EditionID, err)
	case ed != nil:
		return ed.Title
	}
	return ""
}
