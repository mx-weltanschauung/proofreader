package staticsite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"proofreader/internal/site"
	"slices"
	"strings"

	"proofreader/internal/models"
	"proofreader/internal/seo"
)

//go:embed assets/site.css assets/search.js assets/search-page.js
var assetFS embed.FS

// Generator пишет сборку в Out. Out не должен существовать или должен быть
// пуст: генератор не перезаписывает чужие файлы и не дописывает в прежнюю
// сборку.
type Generator struct {
	Src Source
	Out string
	// BaseURL — читальня онлайн, для ссылки «эта страница в читальне онлайн».
	BaseURL string
	// BuildDate — дата в подвале страниц и в PROCHTI.txt.
	BuildDate string
	// EditionID — только это издание (замер); 0 — весь корпус.
	EditionID int64
	// Exclude — работы, которые в архив не идут (снятые по жалобе), вместе
	// с их передними листами; см. ParseExcludeList.
	Exclude map[int64]bool
	// Only — работы, которым вообще можно в архив: каталог боевого вместе с
	// передними листами (cmd/staticsite, флаг -only); nil — все. Работа, которой
	// на боевом нет, в архив не идёт: он не должен опережать сайт.
	Only map[int64]bool
	// Apparatus — работы со снятым аппаратом («N apparatus» в
	// scripts/static-exclude.txt): id → внутренние номера полос, которые
	// снятие аппарата унесло (ApparatusRepository.PageNumbers). Тело работы идёт
	// в архив, эти полосы, её передние листы и статьи её указателя — нет.
	Apparatus map[int64]map[int]bool
	Logf      func(format string, args ...any)

	pages    map[pageKey]pageLoc
	chapters map[int64]string // id главы → путь (у подглавы — с #ch-<id>)
	works    map[int64]string // id работы → её index.html
	concepts map[string]bool
	titles   []titleEntry
	extras   []link // подборки и разборы для главной
	catalog  []int64
}

type pageKey struct {
	work int64
	page int
}

// pageLoc — где лежит полоса: файл и печатный номер (якорь p<n>).
type pageLoc struct {
	file    string
	printed int
}

type link struct{ kind, title, path string }

type editionShelf struct {
	edition *models.Edition
	volumes []*workResult
}

// Built — что попало в сборку: работы каталога (тома и работы вне изданий) и
// все работы вместе с передними листами, по возрастанию, без nil. Зовётся
// после Run; из этого static-release.sh пишет chitalnya-<дата>.build.json, а
// static-publish.sh --withdraw по нему решает, лежит ли работа в архиве.
func (g *Generator) Built() (catalog, all []int64) {
	catalog = append([]int64{}, g.catalog...)
	all = make([]int64, 0, len(g.works))
	for id := range g.works {
		all = append(all, id)
	}
	slices.Sort(catalog)
	slices.Sort(all)
	return catalog, all
}

func (g *Generator) logf(format string, args ...any) {
	if g.Logf != nil {
		g.Logf(format, args...)
	}
}

// Run собирает читальню целиком. Любая ошибка источника прерывает сборку:
// дыру в архиве никто не заметит, пока читатель не упрётся в неё.
func (g *Generator) Run(ctx context.Context) error {
	if entries, err := os.ReadDir(g.Out); err == nil && len(entries) > 0 {
		return fmt.Errorf("каталог %s не пуст", g.Out)
	}
	g.pages = map[pageKey]pageLoc{}
	g.chapters = map[int64]string{}
	g.works = map[int64]string{}
	g.concepts = map[string]bool{}

	shelf, err := g.Src.Shelf(ctx)
	if err != nil {
		return fmt.Errorf("полки: %w", err)
	}
	highlights, err := g.Src.Highlights(ctx)
	if err != nil {
		return fmt.Errorf("избранное: %w", err)
	}

	var shelves []editionShelf
	for _, se := range shelf.Editions {
		if g.EditionID != 0 && se.Edition.ID != g.EditionID {
			continue
		}
		es := editionShelf{edition: se.Edition}
		for _, v := range se.Volumes {
			if v.PagesTotal == 0 {
				continue
			}
			w := v.Work
			res, err := g.workWithFrontMatter(ctx, &w, se.Edition, v)
			if err != nil {
				return err
			}
			if res != nil {
				es.volumes = append(es.volumes, res)
			}
		}
		if len(es.volumes) > 0 {
			shelves = append(shelves, es)
		}
	}

	var loose []*workResult
	if g.EditionID == 0 {
		for _, lw := range shelf.LooseWorks {
			w, err := g.Src.Work(ctx, lw.ID)
			if err != nil {
				return fmt.Errorf("работа %d: %w", lw.ID, err)
			}
			res, err := g.workWithFrontMatter(ctx, w, nil, nil)
			if err != nil {
				return err
			}
			if res != nil {
				loose = append(loose, res)
			}
		}
	}

	for _, es := range shelves {
		if err := g.writeEdition(es, highlights); err != nil {
			return err
		}
	}
	if err := g.writeConcepts(ctx); err != nil {
		return err
	}
	if err := g.writeCollections(ctx); err != nil {
		return err
	}
	if err := g.writeDocuments(ctx); err != nil {
		return err
	}
	if err := g.writeHome(shelves, loose); err != nil {
		return err
	}
	if err := g.writeSearch(); err != nil {
		return err
	}
	return g.writeAssets()
}

func (g *Generator) write(p, content string) error { return g.writeBytes(p, []byte(content)) }

// writeBytes пишет файл сборки. Уже записанный путь — ошибка: два разных
// источника, сошедшиеся в одно имя, иначе молча затёрли бы друг друга.
func (g *Generator) writeBytes(p string, data []byte) error {
	full := filepath.Join(g.Out, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(full); err == nil {
		return fmt.Errorf("файл %s уже записан: два пути сошлись в один", p)
	}
	return os.WriteFile(full, data, 0o644)
}

// editionSlug — адресный слаг издания: url_slug, а пустой — slug. Тем же
// правилом, что effectiveEditionSlug в internal/seo/canonical.go.
func editionSlug(e *models.Edition) string {
	if e.URLSlug != "" {
		return e.URLSlug
	}
	return e.Slug
}

func volumeLabel(w *models.Work) string {
	if w.VolumeNumber == nil {
		return OneLine(w.Title)
	}
	l := fmt.Sprintf("Том %d", *w.VolumeNumber)
	if w.VolumePart != nil && *w.VolumePart != "" {
		l += ", " + *w.VolumePart
	}
	if s := OneLine(w.ShelfLabel); s != "" {
		l += ". " + s
	}
	return l
}

func (g *Generator) writeEdition(es editionShelf, highlights []models.EditionHighlight) error {
	p := EditionIndex(editionSlug(es.edition))
	var b strings.Builder
	fmt.Fprintf(&b, "<h1>%s</h1>\n", Esc(OneLine(es.edition.Title)))
	if d := OneLine(es.edition.Description); d != "" {
		fmt.Fprintf(&b, "<p>%s</p>\n", Esc(d))
	}
	var picks []string
	for _, h := range highlights {
		if h.EditionID != es.edition.ID {
			continue
		}
		target, ok := g.chapters[h.ChapterID]
		if !ok {
			continue
		}
		label := OneLine(h.Label)
		if label == "" {
			label = OneLine(h.ChapterTitle)
		}
		picks = append(picks, fmt.Sprintf(`<a href="%s">%s</a>`, Esc(rel(p, target)), Esc(label)))
	}
	if len(picks) > 0 {
		b.WriteString(`<p class="highlights">Избранное: ` + strings.Join(picks, " · ") + "</p>\n")
	}
	b.WriteString(`<div class="shelf"><ul>` + "\n")
	for _, v := range es.volumes {
		fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`+"\n", Esc(rel(p, v.index)), Esc(volumeLabel(v.work)))
	}
	b.WriteString("</ul></div>\n")
	return g.write(p, g.page(shell{
		path: p, title: es.edition.Title, body: b.String(),
		prod: fmt.Sprintf("/editions/%d-%s", es.edition.ID, editionSlug(es.edition)),
	}))
}

func (g *Generator) writeHome(shelves []editionShelf, loose []*workResult) error {
	var b strings.Builder
	b.WriteString("<h1>" + Esc(site.Name()) + "</h1>\n")
	fmt.Fprintf(&b, "<p>Статическая копия читальни от %s: весь текст, без сканов страниц. "+
		"Открывается с диска и с любого хостинга; как включить поиск по тексту — в PROCHTI.txt.</p>\n", Esc(g.BuildDate))
	fmt.Fprintf(&b, `<p><a href="%s">Предметный указатель</a> · <a href="%s">Поиск</a></p>`+"\n",
		Esc(rel(HomeFile, ConceptsIndex)), Esc(rel(HomeFile, SearchFile)))
	b.WriteString(`<div class="shelf">` + "\n")
	for _, es := range shelves {
		fmt.Fprintf(&b, `<h2><a href="%s">%s</a></h2>`+"\n<ul>\n",
			Esc(rel(HomeFile, EditionIndex(editionSlug(es.edition)))), Esc(OneLine(es.edition.Title)))
		for _, v := range es.volumes {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`+"\n", Esc(rel(HomeFile, v.index)), Esc(volumeLabel(v.work)))
		}
		b.WriteString("</ul>\n")
	}
	if len(loose) > 0 {
		b.WriteString("<h2>Вне собраний</h2>\n<ul>\n")
		for _, v := range loose {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`+"\n", Esc(rel(HomeFile, v.index)), Esc(OneLine(v.work.Title)))
		}
		b.WriteString("</ul>\n")
	}
	for _, kind := range []struct{ key, heading string }{{"collection", "Подборки"}, {"document", "Разборы"}} {
		var items []link
		for _, l := range g.extras {
			if l.kind == kind.key {
				items = append(items, l)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "<h2>%s</h2>\n<ul>\n", kind.heading)
		for _, l := range items {
			fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`+"\n", Esc(rel(HomeFile, l.path)), Esc(OneLine(l.title)))
		}
		b.WriteString("</ul>\n")
	}
	b.WriteString("</div>\n")
	return g.write(HomeFile, g.page(shell{path: HomeFile, body: b.String(), prod: "/"}))
}

func (g *Generator) writeAssets() error {
	for _, name := range []string{assetCSS, assetSearch, assetSearchPage} {
		data, err := assetFS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := g.writeBytes(name, data); err != nil {
			return err
		}
	}
	fonts, err := fs.ReadDir(seo.Fonts, "assets")
	if err != nil {
		return err
	}
	for _, f := range fonts {
		data, err := seo.Fonts.ReadFile(path.Join("assets", f.Name()))
		if err != nil {
			return err
		}
		if err := g.writeBytes(path.Join("assets/fonts", f.Name()), data); err != nil {
			return err
		}
	}
	return g.write("PROCHTI.txt", g.readme())
}

func (g *Generator) readme() string {
	text := strings.ReplaceAll(readmeTemplate, "{date}", g.BuildDate)
	text = strings.ReplaceAll(text, "{name}", site.Name())
	return strings.ReplaceAll(text, "{base}", g.BaseURL)
}

const readmeTemplate = `{name} — статическая копия от {date}

Это весь текст читальни {base} папкой файлов: издания, тома, главы
со сносками и номерами страниц, предметный указатель, подборки и разборы.
Сканов страниц здесь нет.

Как открыть

1. Дважды щёлкните index.html. Читать, переходить по оглавлению и
   указателю, искать по заглавиям можно сразу, без интернета.

2. Чтобы работал поиск по тексту, папку должен открыть сервер:
   — Windows: запустите serve-windows.exe;
   — macOS: serve-macos (Apple M1 и новее) или serve-macos-intel;
     при первом запуске macOS спросит разрешения: правый щелчок → «Открыть»;
   — Linux: ./serve-linux.
   Программа сама откроет браузер и напечатает адрес. Закройте её окно,
   чтобы остановить. Подойдёт и любой веб-сервер, например
   «python3 -m http.server» в этой папке и адрес http://localhost:8000/.

3. Положить на хостинг (GitHub Pages, nginx и любой другой) можно как
   есть: все ссылки относительные.

Откуда текст и на каких условиях, как пожаловаться правообладателю:
{base}/legal#copyright
`
