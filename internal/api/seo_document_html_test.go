package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/seo"
	"proofreader/pkg/markdown"
)

// Страница краулера — ВТОРОЙ потребитель авторского markdown после /view, и
// радиус поражения у неё больше, а не меньше: её читают поисковики и
// краулеры мессенджеров, то есть ссылку на разбор разворачивает у себя в
// чате в том числе модератор, который этот разбор и рассматривает.
//
// Сторож стоит ЗДЕСЬ, а не в internal/seo, и это расхождение с брифом задачи
// названо вслух: в пакете seo источник разборов подставной (иначе импорт
// api замкнул бы цикл), и тест там проверял бы подделку, а не рендерер —
// мутация ForUntrustedAuthor() → r в assembleDocument его бы не тронула.
// Здесь собрана вся цепочка целиком: настоящий SEODocumentSource →
// assembleDocumentFor → assembleDocument → seo.Source.Document.
//
// Разбирается готовая вёрстка (unsafeMarkupIn, executable_markup_probe_test.go),
// а не ищутся подстроки: со снятым parser.Attributes строка `{onclick="…"}`
// остаётся в выводе безобидным ТЕКСТОМ абзаца, и подстрочная проверка врёт
// в обе стороны (урок C1 ветки 2).
func TestSEODocumentPageCarriesNoExecutableAuthorHTML(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"сырым HTML", authorHTMLPayload},
		{"родными средствами markdown", authorNativePayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := seoDocumentPageFor(t, tc.body)

			if bad := unsafeMarkupIn(doc.Body); len(bad) > 0 {
				t.Errorf("страница краулера несёт исполняемое: %v\n%s", bad, doc.Body)
			}
			// Подавление не вправе съедать сам разбор.
			if !strings.Contains(doc.Body, "Начало разбора") ||
				!strings.Contains(doc.Body, "Конец разбора") {
				t.Fatalf("подавление съело текст автора:\n%s", doc.Body)
			}
		})
	}
}

// Контрольная группа: подавление бьёт по исполняемому, а не по разметке
// вообще. Без неё «починка», выкинувшая из авторского текста всё подряд,
// прошла бы зелёной — а краулер получал бы пустую страницу вместо разбора.
func TestSEODocumentPageKeepsHarmlessAuthorMarkup(t *testing.T) {
	const body = "*курсив*, **жирный** и [обычная ссылка](https://example.org).\n\n" +
		"# Заголовок\n\n" +
		"> Цитата\n\n" +
		"| а | б |\n| --- | --- |\n| 1 | 2 |\n\n" +
		"Текст до ![выброшенная](https://example.org/p.gif) текст после.\n\n" +
		"Сноска автора[^1].\n\n[^1]: тело сноски\n"

	doc := seoDocumentPageFor(t, body)

	for _, want := range []string{
		"<em>курсив</em>", "<strong>жирный</strong>",
		`href="https://example.org"`, "<h1", "<blockquote>",
		"<table>", "<td", "тело сноски",
		// Картинка выброшена, а текст ВОКРУГ неё — нет: подавление снимает
		// подресурс, а не абзац, в котором он стоял.
		"Текст до", "текст после.",
	} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("подавление задело безобидную разметку, нет %q:\n%s", want, doc.Body)
		}
	}
	if bad := unsafeMarkupIn(doc.Body); len(bad) > 0 {
		t.Errorf("в безобидной фикстуре осталось недоверенное: %v\n%s", bad, doc.Body)
	}
}

// Вторая контрольная группа, зеркальная: корпус этого подавления не
// касается. Вклейка идёт прежним рендерером, и ручная таблица тома вместе с
// её сноской обязана доехать до краулера как есть — иначе «починка»
// авторского текста тихо испортила бы весь корпус.
func TestSEODocumentPageKeepsCorpusCutIntact(t *testing.T) {
	doc := seoDocumentPageFor(t, "Моя мысль.\n\n<cut id=\"7\">\n\nПосле вклейки.\n")

	for _, want := range []string{"<table>", "<td>", "товар", "1901", "fnref:v1-4-r1"} {
		if !strings.Contains(doc.Body, want) {
			t.Errorf("корпусная разметка вклейки пострадала, нет %q:\n%s", want, doc.Body)
		}
	}
}

// seoDocumentPageFor прогоняет тело разбора через ВСЮ цепочку страницы
// краулера и отдаёт готовый seo.Doc.
//
// Тело кладётся в ОДОБРЕННУЮ редакцию (PublishedMarkdown): краулер видит
// только её, и положи его в черновик — сторож проверял бы пустую страницу и
// был бы зелен по неверной причине.
func seoDocumentPageFor(t *testing.T, body string) *seo.Doc {
	t.Helper()

	cut := &models.DocumentCut{
		ID: 7, DocumentID: 1, WorkID: ptrInt64(1),
		Anchor: models.Anchor{StartPageID: 10, StartOffset: 0, EndPageID: 10,
			EndOffset: len(corpusCutMarkdown)},
		Status: models.CutStatusOK, SourceTitle: "Ленин. Что делать?",
	}
	cuts := newFakeDocumentCutStore(cut)
	cuts.pagesByCutID = map[int64][]*models.Page{7: {cutPage(10, 4, corpusCutMarkdown)}}

	published := time.Unix(1_700_000_000, 0)
	store := &fakeDocumentStore{doc: &models.Document{
		ID: 1, Slug: "chto-delat", AuthorNickname: "чтец", OwnerID: ptrInt64(5),
		Title: body, MarkdownContent: body,
		PublishedTitle: "Что делать?", PublishedMarkdown: body,
		PublishedAt: &published, WasPublished: true,
		ReviewStatus: models.DocumentApproved,
	}}

	src := &seo.Source{
		BaseURL:  "https://lib.example.org",
		Renderer: markdown.NewRenderer(),
		Documents: NewSEODocumentSource(store, cuts, seoDocWorks(),
			markdown.NewRenderer()),
	}

	doc, err := src.Document(context.Background(), "чтец", "chto-delat")
	if err != nil {
		t.Fatalf("seo.Source.Document: %v", err)
	}
	return doc
}
