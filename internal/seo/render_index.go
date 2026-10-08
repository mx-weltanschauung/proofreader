package seo

import (
	"context"
	"fmt"
	"proofreader/internal/site"
	"strings"

	"proofreader/internal/models"
)

// conceptListCap — сколько понятий печатается в списке. Полный обход корпуса
// краулер делает по карте сайта (sitemap-concepts.xml), где лежат все адреса
// без исключения; список — страница для человека и точка входа, и складывать
// в неё тысячи строк незачем.
const conceptListCap = 500

// Concept — статья предметного указателя: текст и адреса в корпусе.
//
// Понятие-перенаправление по прямому адресу всё равно достижимо — кто-то мог
// сохранить ссылку, — и в индекс список его не пускает (см. ConceptList), но
// здесь мы его не 404-им: страница есть, только пустая и без адресов. Отдаём
// RobotsNoIndex, чтобы краулер не индексировал заголовок без содержимого.
// Собрать полноценное «см. такое-то» сейчас нечем: ConceptSource не
// дотягивает Links, и расширять интерфейс ради одной строки не будем.
func (s *Source) Concept(ctx context.Context, slug string) (*Doc, error) {
	c, err := s.Concepts.GetConceptBySlug(ctx, slug)
	if isNotFound(err) {
		return nil, notFound("понятие %q", slug)
	}
	if err != nil {
		return nil, fmt.Errorf("понятие %q: %w", slug, err)
	}
	if c == nil {
		return nil, notFound("понятие %q", slug)
	}

	// Понятие расщеплено на понятие каталога и статьи указателей (задача 6):
	// у него может быть несколько статей, и печатать надо адреса ВСЕХ, а не
	// только первой. Индексируется понятие, если индексируется хоть одна
	// статья — чистое перенаправление (Kind == redirect) без содержимого
	// смысла индексировать не имеет.
	//
	// Прежние поля понятия (Kind, ArticleMarkdown, References) — временная
	// совместимость, заполнявшаяся первой статьёй, — сняты миграцией 000026
	// (задача 11) вовсе: они принадлежат статье, а не понятию, и код на них
	// выглядел бы рабочим, пока у понятия ровно одна статья, и ломался бы
	// молча, как только появляется вторая.
	robots := RobotsNoIndex
	for _, a := range c.Articles {
		if a.Kind == models.IndexConceptKindArticle {
			robots = RobotsIndex
			break
		}
	}

	// Renderer.Render отдаёт XHTML-фрагмент: обёртку документа снимает сам
	// pkg/markdown. Раньше её снимали здесь, потому что вложенный в тело
	// страницы второй <title> ломает разметку для простых сборщиков превью
	// (см. F1 итогового ревью). Причина никуда не делась — переехало место,
	// где обёртка снимается. Про ошибку разбора см. комментарий в
	// render_text.go: её ветка недостижима, и потому её здесь больше нет.
	var body strings.Builder
	var articleHTML string
	total := 0
	body.WriteString("<h1>" + Esc(OneLine(c.Title)) + "</h1>\n")
	for _, a := range c.Articles {
		html := s.Renderer.Render(a.ArticleMarkdown)
		if articleHTML == "" {
			articleHTML = html
		}
		// Заголовок статьи печатается, только когда статей несколько:
		// понятие с одной статьёй (сегодня — все понятия корпуса) не должно
		// обрастать лишним <h2> с названием единственного издания.
		if len(c.Articles) > 1 && a.EditionTitle != "" {
			body.WriteString("<h2>" + Esc(OneLine(a.EditionTitle)) + "</h2>\n")
		}
		body.WriteString(html + "\n")
		if len(a.References) > 0 {
			body.WriteString("<h2>Адреса в корпусе</h2>\n<ul>\n")
			for _, ref := range a.References {
				body.WriteString("<li>" + Esc(referenceLine(ref)) + "</li>\n")
			}
			body.WriteString("</ul>\n")
			total += len(a.References)
		}
	}

	alternate := s.abs(chapterTextPath(conceptBase(slug), 1))
	return &Doc{
		Title: OneLine(c.Title) + " — предметный указатель — " + site.Name(),
		Description: sentences(
			Excerpt(articleHTML, descriptionRunes),
			fmt.Sprintf("Адресов в корпусе: %d", total),
		),
		Canonical: s.abs("/concepts/" + slug),
		OGType:    "article",
		ImageURL:  s.abs("/og/concept/" + slug + ".png"),
		Alternate: alternate,
		Robots:    robots,
		JSONLD: map[string]any{
			"@context":   "https://schema.org",
			"@type":      "DefinedTerm",
			"name":       OneLine(c.Title),
			"inLanguage": "ru",
			"url":        s.abs("/concepts/" + slug),
		},
		Body:     llmNote(alternate) + body.String(),
		CacheKey: c.UpdatedAt,
	}, nil
}

// referenceLine — адрес указателя одной строкой: «как субстанция стоимости —
// т. 23, с. 45—48». Номера печатные, диапазон из одной страницы печатается
// одним числом.
func referenceLine(ref *models.IndexReference) string {
	pages := fmt.Sprintf("с. %d", ref.PageStart)
	if ref.PageEnd > ref.PageStart {
		pages = fmt.Sprintf("с. %d—%d", ref.PageStart, ref.PageEnd)
	}
	volume := fmt.Sprintf("т. %d", ref.VolumeNumber)
	if ref.VolumePart != nil && *ref.VolumePart != "" {
		volume += " ч. " + *ref.VolumePart
	}
	return sentences(OneLine(ref.Rubric), volume+", "+pages, OneLine(ref.Note))
}

// Collection — подборка: оглавление уже собрано на входе (см.
// api.SEOCollectionSource, задача 10), здесь оно только печатается.
//
// nickname пуст у сотруднической подборки (первый, короткий адрес) и
// заполнен у читательской (второй адрес, /collections/{ник}/{слаг}: слаг
// уникален только в паре с ником). PublishedAt == nil делится на два разных
// ответа краулеру, а не один: черновик, который не публиковался НИКОГДА,
// снаружи не существует вовсе (404); подборка, снятая с публикации уже после
// того, как побывала опубликованной, отвечает честным «было и снято» (410).
// Различает их PublishIPHash — см. ветку ниже.
func (s *Source) Collection(ctx context.Context, nickname, slug string) (*Doc, error) {
	c, err := s.Collections.GetByAuthorSlug(ctx, nickname, slug)
	if isNotFound(err) {
		return nil, notFound("подборка %q", slug)
	}
	if err != nil {
		return nil, fmt.Errorf("подборка %q: %w", slug, err)
	}
	if c == nil {
		return nil, notFound("подборка %q", slug)
	}
	if c.PublishedAt == nil {
		// PublishIPHash переживает снятие с публикации (см. его doc-комментарий
		// в models.Collection) — пустой хэш отличает черновик, который не
		// публиковался НИКОГДА, от подборки, снятой уже после публикации.
		// Черновику нужен 404 (ErrNeverPublished), а не 410: 410 значит «было
		// и снято» и тем самым подтвердил бы краулеру, что черновик вообще
		// существует. Зеркалит internal/api/collection_handler.go (Get).
		if c.PublishIPHash == "" {
			return nil, neverPublished("подборка %q", slug)
		}
		return nil, notFound("подборка %q", slug)
	}

	path := "/collections/" + slug
	if c.AuthorNickname != "" {
		path = "/collections/" + c.AuthorNickname + "/" + slug
	}

	title := OneLine(c.Title) + " — подборка — " + site.Name()
	// Несъёмная пометка происхождения: читальня не модерирует читательские
	// подборки, поэтому обязана явно показать, кто их собрал, — и в заголовке
	// страницы, и в подписи карточки (card.go, cardInput). Правится только
	// здесь, не полем на стороне подборки — автор пометку убрать не может.
	if c.AuthorNickname != "" {
		title = OneLine(c.Title) + " — подборка читателя " + c.AuthorNickname + " — " + site.Name()
	}

	var body strings.Builder
	body.WriteString("<h1>" + Esc(OneLine(c.Title)) + "</h1>\n")
	if c.AuthorNickname != "" {
		body.WriteString("<p>Собрал читатель " + Esc(c.AuthorNickname) + "</p>\n")
	}
	if c.Description != "" {
		body.WriteString("<p>" + Esc(OneLine(c.Description)) + "</p>\n")
	}
	live := 0
	body.WriteString("<ul>\n")
	for _, item := range c.Items {
		label := Esc(OneLine(item.Title))
		if a := item.ResolvedAuthor(); a != "" {
			label = Esc(OneLine(a)) + ". " + label
		}
		// Битый пункт (источник удалён) остаётся в оглавлении, но ссылкой не
		// становится: чтение такого пункта отвечает 410, и вести туда
		// краулера — кормить его битыми адресами.
		if item.Broken {
			fmt.Fprintf(&body, "<li>%s</li>\n", label)
			continue
		}
		fmt.Fprintf(&body, `<li><a href="%s/read/%d">%s</a></li>`+"\n",
			Esc(path), item.ID, label)
		live++
	}
	body.WriteString("</ul>\n")

	// Читательская подборка: видна по прямой ссылке (страница есть, карточка
	// рисуется), но не в витрине и не в карте сайта — без модерации спам
	// уехал бы прямиком в показ. RobotsNoIndex не запрещает обход (это делал
	// бы robots.txt, который её тогда закрыл бы и от noindex), а прямо
	// говорит поисковику не класть страницу в индекс.
	robots := RobotsIndex
	imgPath := "/og/collection/" + slug + ".png"
	if c.AuthorNickname != "" {
		robots = RobotsNoIndex
		imgPath = "/og/collection/" + c.AuthorNickname + "/" + slug + ".png"
	}

	return &Doc{
		Title: title,
		Description: sentences(OneLine(c.Title), OneLine(c.Description),
			fmt.Sprintf("Пунктов в подборке: %d", live)),
		Canonical: s.abs(path),
		OGType:    "website",
		ImageURL:  s.abs(imgPath),
		Robots:    robots,
		JSONLD: map[string]any{
			"@context":   "https://schema.org",
			"@type":      "CollectionPage",
			"name":       OneLine(c.Title),
			"inLanguage": "ru",
			"url":        s.abs(path),
		},
		Body:     body.String(),
		CacheKey: c.UpdatedAt,
	}, nil
}

// ConceptList — предметный указатель списком, точка входа для человека.
//
// Понятия-перенаправления сюда не идут: в отличие от карты сайта (задача 2,
// свой запрос с фильтром kind = 'article'), ListConcepts фильтра по Kind не
// знает и отдаёт всё — здесь отсев нужен явно, иначе список линковал бы
// заголовки без тела (Concept отдаёт их RobotsNoIndex, но битую ссылку в
// собственном списке это не оправдывает).
func (s *Source) ConceptList(ctx context.Context) (*Doc, error) {
	concepts, err := s.Concepts.ListConcepts(ctx, "", "", conceptListCap, 0)
	if err != nil {
		return nil, fmt.Errorf("список понятий: %w", err)
	}

	var body strings.Builder
	body.WriteString("<h1>Предметный указатель</h1>\n<ul>\n")
	for _, c := range concepts {
		if c.Kind != models.IndexConceptKindArticle {
			continue
		}
		fmt.Fprintf(&body, `<li><a href="/concepts/%s">%s</a></li>`+"\n",
			Esc(c.Slug), Esc(OneLine(c.Title)))
	}
	body.WriteString("</ul>\n")

	alternate := s.abs("/concepts.md")
	return &Doc{
		Title:       "Предметный указатель — " + site.Name(),
		Description: "Понятия корпуса читальни с адресами в томах.",
		Canonical:   s.abs("/concepts"),
		Alternate:   alternate,
		ImageURL:    s.abs("/og/site/concepts.png"),
		OGType:      "website",
		Robots:      RobotsIndex,
		Body:        llmNote(alternate) + body.String(),
	}, nil
}

// CollectionList — подборки списком.
func (s *Source) CollectionList(ctx context.Context) (*Doc, error) {
	collections, err := s.Collections.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("список подборок: %w", err)
	}

	var body strings.Builder
	body.WriteString("<h1>Подборки</h1>\n<ul>\n")
	for _, c := range collections {
		fmt.Fprintf(&body, `<li><a href="/collections/%s">%s</a></li>`+"\n",
			Esc(c.Slug), Esc(OneLine(c.Title)))
	}
	body.WriteString("</ul>\n")

	return &Doc{
		Title:       "Подборки — " + site.Name(),
		Description: "Читательские подборки глав и работ со всего корпуса.",
		Canonical:   s.abs("/collections"),
		ImageURL:    s.abs("/og/site/collections.png"),
		OGType:      "website",
		Robots:      RobotsIndex,
		Body:        body.String(),
	}, nil
}

// Home — точка входа для краулера: издания ссылками. Полки с обрезами томов,
// которые рисует SPA, краулеру не нужны — ему нужны адреса.
func (s *Source) Home(ctx context.Context) (*Doc, error) {
	editions, err := s.Editions.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("издания: %w", err)
	}

	var body strings.Builder
	body.WriteString("<h1>" + Esc(site.Name()) + "</h1>\n")
	body.WriteString("<p>" + Esc(site.Description()) + "</p>\n")
	body.WriteString("<ul>\n")
	for _, ed := range editions {
		// effectiveEditionSlug — тот же откат url_slug → slug, что и в самой
		// карточке издания (render_volume.go, Edition), одним помощником в
		// canonical.go: иначе после задачи 10 главная кормила бы краулера
		// лишним 301 на каждое собрание — а она первая страница на пути
		// обхода всего корпуса.
		fmt.Fprintf(&body, `<li><a href="%s">%s</a></li>`+"\n",
			editionPath(ed.ID, effectiveEditionSlug(ed)), Esc(OneLine(ed.Title)))
	}
	body.WriteString(`</ul>
<ul>
<li><a href="/concepts">Предметный указатель</a></li>
<li><a href="/collections">Подборки</a></li>
<li><a href="/documents">Разборы</a></li>
</ul>
`)

	return &Doc{
		Title:       site.Name(),
		Description: site.Description(),
		Canonical:   s.abs("/"),
		ImageURL:    s.abs("/og/site/home.png"),
		OGType:      "website",
		Robots:      RobotsIndex,
		JSONLD: map[string]any{
			"@context":   "https://schema.org",
			"@type":      "WebSite",
			"name":       site.Name(),
			"inLanguage": "ru",
			"url":        s.abs("/"),
		},
		Body: body.String(),
	}, nil
}
