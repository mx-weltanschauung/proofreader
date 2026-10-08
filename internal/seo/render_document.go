package seo

import (
	"context"
	"fmt"
	"proofreader/internal/site"
	"strings"
	"time"
)

// documentListCap — сколько разборов печатается в витрине. Список — точка
// входа для человека, а не способ обойти всё: полный обход краулер делает по
// карте сайта, куда разборы дописывает задача 8. Тот же довод и то же число,
// что у conceptListCap.
const documentListCap = 500

// Document — разбор читателя или редакции: авторский текст с вклеенными
// кусками корпуса.
//
// nickname пуст у сотруднического разбора (первый, короткий адрес
// /documents/{слаг}) и заполнен у читательского (второй адрес
// /documents/{ник}/{слаг}: слаг уникален только в паре с подписью). Тело
// приходит СОБРАННЫМ из api.SEODocumentSource — здесь оно только
// печатается. Собирать его тут нечем и не нужно: текст автора пишет вошедший
// читатель и обязан идти через markdown.Renderer.ForUntrustedAuthor(),
// который стоит внутри api.assembleDocument, а второй путь рендера открыл бы
// четыре закрытых стока заново — на этот раз в странице, которую читает
// поисковик и мессенджер модератора.
//
// Отсутствие приходит сюда уже разделённым на две ветки (см. Page/Card
// переводчика): черновик, которого снаружи не существует вовсе, —
// ErrNeverPublished (404), снятый с публикации — ErrNotFound (410). Тот же
// разбор, что у подборки, и по тем же причинам.
func (s *Source) Document(ctx context.Context, nickname, slug string) (*Doc, error) {
	page, err := s.Documents.Page(ctx, nickname, slug)
	if err != nil {
		return nil, documentError(err, slug)
	}
	if page == nil || page.Document == nil {
		return nil, notFound("разбор %q", slug)
	}
	d := page.Document

	// Адрес строится по подписи САМОЙ строки, а не по той, что спросили:
	// короткий адрес сотруднического разбора приходит сюда с пустым ником, и
	// брать его из запроса значило бы верить запросу там, где есть данные.
	path := documentPath(d.AuthorNickname, d.Slug)

	title := OneLine(d.Title) + " — разбор — " + site.Name()
	if d.AuthorNickname != "" {
		title = OneLine(d.Title) + " — разбор читателя " + d.AuthorNickname + " — " + site.Name()
	}

	var body strings.Builder
	body.WriteString("<h1>" + Esc(OneLine(d.Title)) + "</h1>\n")
	// Несъёмная пометка происхождения, как у подборки: читальня показывает,
	// кто собрал текст, и автор её убрать не может — она печатается здесь, а
	// не берётся полем из тела разбора.
	if d.AuthorNickname != "" {
		body.WriteString("<p>Собрал читатель " + Esc(d.AuthorNickname) + "</p>\n")
	}
	body.WriteString(page.BodyHTML)

	jsonld := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "Article",
		"headline":   OneLine(d.Title),
		"inLanguage": "ru",
		"url":        s.abs(path),
	}
	if d.AuthorNickname != "" {
		jsonld["author"] = map[string]any{"@type": "Person", "name": d.AuthorNickname}
	}
	if d.PublishedAt != nil {
		jsonld["datePublished"] = d.PublishedAt.UTC().Format(time.RFC3339)
	}

	return &Doc{
		Title:       title,
		Description: Excerpt(page.BodyHTML, descriptionRunes),
		Canonical:   s.abs(path),
		OGType:      "article",
		ImageURL:    s.abs(documentCardPath(d.AuthorNickname, d.Slug)),
		// ОБЕ разновидности разбора индексируются — и читательская тоже. Это
		// единственное место, где разбор расходится с подборкой, и разошёлся
		// он не по недосмотру: читательская подборка не модерируется и
		// потому живёт только по прямой ссылке с RobotsNoIndex, а разбор
		// проходит модерацию — опубликованное уже просмотрено редактором
		// (решение владельца 19.09.2026). Не «чинить» по образцу подборки.
		Robots:   RobotsIndex,
		JSONLD:   jsonld,
		Body:     body.String(),
		CacheKey: d.UpdatedAt,
	}, nil
}

// DocumentList — витрина разборов, точка входа для человека.
//
// Отбор делает ListPublished переводчика: черновик и снятое сюда не попадают
// вовсе — отдельным методом хранилища, а не фильтром над общим списком, по
// той же причине, по какой так устроена витрина в API (первая же забытая
// проверка в обработчике превратила бы список в утечку черновиков).
func (s *Source) DocumentList(ctx context.Context) (*Doc, error) {
	documents, err := s.Documents.ListPublished(ctx, documentListCap, 0)
	if err != nil {
		return nil, fmt.Errorf("список разборов: %w", err)
	}

	var body strings.Builder
	body.WriteString("<h1>Разборы</h1>\n<ul>\n")
	for _, d := range documents {
		label := Esc(OneLine(d.Title))
		if d.AuthorNickname != "" {
			label += " — собрал читатель " + Esc(d.AuthorNickname)
		}
		fmt.Fprintf(&body, `<li><a href="%s">%s</a></li>`+"\n",
			Esc(documentPath(d.AuthorNickname, d.Slug)), label)
	}
	body.WriteString("</ul>\n")

	return &Doc{
		Title:       "Разборы — " + site.Name(),
		Description: "Разборы читателей и редакции: авторский текст с вклеенными кусками корпуса.",
		Canonical:   s.abs("/documents"),
		ImageURL:    s.abs("/og/site/documents.png"),
		OGType:      "website",
		Robots:      RobotsIndex,
		Body:        body.String(),
	}, nil
}

// documentError переводит ошибку переводчика в ошибку рендерера, не теряя
// различия между «никогда не публиковался» (404) и «было и снято» (410).
//
// ErrNeverPublished проверяется ПЕРВЫМ и отдаётся как есть: isNotFound
// смотрит в том числе на суффикс "not found" текста ошибки, и сообщение
// черновика, случись в нём такой хвост, уехало бы в ветку 410 — то есть
// подтвердило бы краулеру существование черновика ровно тем кодом, которого
// решение не допускает.
func documentError(err error, slug string) error {
	if isNeverPublished(err) {
		return err
	}
	if isNotFound(err) {
		return notFound("разбор %q", slug)
	}
	return fmt.Errorf("разбор %q: %w", slug, err)
}
