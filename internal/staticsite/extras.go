package staticsite

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"proofreader/pkg/book"
)

func (g *Generator) writeCollections(ctx context.Context) error {
	list, err := g.Src.Collections(ctx)
	if err != nil {
		return fmt.Errorf("подборки: %w", err)
	}
	for _, c := range list {
		p := CollectionFile(c.ID, c.Slug)
		var b strings.Builder
		fmt.Fprintf(&b, "<h1>%s</h1>\n", Esc(OneLine(c.Title)))
		if d := OneLine(c.Description); d != "" {
			fmt.Fprintf(&b, "<p>%s</p>\n", Esc(d))
		}
		b.WriteString("<ol>\n")
		for _, it := range c.Items {
			title := Esc(OneLine(it.Title))
			author := ""
			if a := OneLine(it.Author); a != "" {
				author = ` <span class="author">— ` + Esc(a) + `</span>`
			}
			target := g.itemTarget(it)
			switch {
			case it.Broken:
				fmt.Fprintf(&b, `<li class="broken">%s%s <span class="note">(источник недоступен)</span></li>`+"\n", title, author)
			case target == "":
				fmt.Fprintf(&b, `<li class="absent">%s%s <span class="note">(нет в этой копии)</span></li>`+"\n", title, author)
			default:
				fmt.Fprintf(&b, `<li><a href="%s">%s</a>%s</li>`+"\n", Esc(rel(p, target)), title, author)
			}
		}
		b.WriteString("</ol>\n")
		if err := g.write(p, g.page(shell{path: p, title: c.Title, body: b.String(), prod: "/collections/" + c.Slug})); err != nil {
			return err
		}
		g.extras = append(g.extras, link{"collection", c.Title, p})
		g.addTitle("collection", c.Title, "Подборка", p)
	}
	return nil
}

// itemTarget — куда ведёт пункт подборки. Глава без своего якоря в сборке
// (съедена соседом при раскладке тома) ведёт на свою первую полосу.
func (g *Generator) itemTarget(it CollectionItem) string {
	if it.Broken {
		return ""
	}
	if it.ChapterID != 0 {
		if p, ok := g.chapters[it.ChapterID]; ok {
			return p
		}
		if loc, ok := g.pages[pageKey{it.WorkID, it.StartPage}]; ok {
			return loc.file + "#" + book.PageAnchor(loc.printed)
		}
		return ""
	}
	return g.works[it.WorkID]
}

// cutPagePath — ссылка вклейки на полосу тома: так её печатает сборка
// разбора (internal/api/document_render.go), номер — внутренний.
var cutPagePath = regexp.MustCompile(`^/works/(\d+)/pages/(\d+)$`)

// anchorTag — ссылка в теле разбора с любыми атрибутами вокруг href.
var anchorTag = regexp.MustCompile(`<a ([^>]*?)href="([^"]*)"([^>]*)>(.*?)</a>`)

// relinkDocument переводит ссылки тела разбора так, чтобы копия не ходила в
// сеть и не вела в никуда. Тело пишет читатель (ForUntrustedAuthor оставляет
// ему обычные ссылки), а вклейки несут вёрстку корпуса:
//   - ссылка вклейки на полосу — на полосу сборки; полосы нет — без href;
//   - путь читальни («/works/…») и адрес самой читальни — на читальню онлайн;
//   - прочий внешний адрес — текстом рядом с подписью: копия офлайн-первая;
//   - всё остальное (относительная «ссылка», «[x](y)» из текста корпуса) —
//     текстом, как записано;
//   - обратная ссылка примечания без места в тексте — номером.
func (g *Generator) relinkDocument(p, body string) string {
	base := strings.TrimSuffix(g.BaseURL, "/")
	body = anchorTag.ReplaceAllStringFunc(body, func(m string) string {
		sub := anchorTag.FindStringSubmatch(m)
		pre, href, post, inner := sub[1], sub[2], sub[3], sub[4]
		raw := html.UnescapeString(href)
		if c := cutPagePath.FindStringSubmatch(raw); c != nil {
			work, _ := strconv.ParseInt(c[1], 10, 64)
			pageNo, _ := strconv.Atoi(c[2])
			if loc, ok := g.pages[pageKey{work, pageNo}]; ok {
				return `<a ` + pre + `href="` + Esc(rel(p, loc.file+"#"+book.PageAnchor(loc.printed))) + `"` + post + `>` + inner + `</a>`
			}
			return `<a ` + pre + `data-missing-page="` + c[2] + `"` + post + `>` + inner + `</a>`
		}
		switch {
		case strings.HasPrefix(raw, "#"):
			return m
		case base != "" && strings.HasPrefix(raw, base+"/"):
			return m
		case strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && base != "":
			return `<a ` + pre + `href="` + Esc(base+raw) + `"` + post + `>` + inner + `</a>`
		case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"):
			return inner + ` <span class="link-url">(` + href + `)</span>`
		default:
			return "[" + inner + "](" + href + ")"
		}
	})
	return dropInertBackLinks(body)
}

func (g *Generator) writeDocuments(ctx context.Context) error {
	list, err := g.Src.Documents(ctx)
	if err != nil {
		return fmt.Errorf("разборы: %w", err)
	}
	for _, d := range list {
		p := DocumentFile(d.ID, d.Slug)
		var b strings.Builder
		fmt.Fprintf(&b, "<h1>%s</h1>\n", Esc(OneLine(d.Title)))
		if a := OneLine(d.Author); a != "" {
			fmt.Fprintf(&b, `<p class="author">%s</p>`+"\n", Esc(a))
		}
		b.WriteString(`<div class="document">` + g.relinkDocument(p, d.BodyHTML) + "</div>\n")
		prod := "/documents/" + d.Slug
		if d.Author != "" {
			prod = "/documents/" + d.Author + "/" + d.Slug
		}
		if err := g.write(p, g.page(shell{path: p, title: d.Title, body: b.String(), prod: prod})); err != nil {
			return err
		}
		g.extras = append(g.extras, link{"document", d.Title, p})
		g.addTitle("document", d.Title, "Разбор", p)
	}
	return nil
}
