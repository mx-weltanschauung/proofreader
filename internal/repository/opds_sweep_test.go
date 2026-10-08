package repository_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/api"
	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/models"
	"proofreader/internal/opds"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/pkg/markdown"
)

const sweepBase = "https://lib.example.org"

// opdsFixture — том неудобного вида (верхний уровень — годы, внутри —
// работы), его передние листы и собрание. Главы заводятся SQL-ом, а не
// ChapterRepository.Create: тот спрашивает классификатор аппарата, а
// фикстуре признак нужен ровно такой, какой написан.
type opdsFixture struct {
	edition, volume, front int64
	years, letter, essay   int64
	leaf, notes            int64
}

func seedOPDSFixture(t *testing.T, pool *pgxpool.Pool) opdsFixture {
	t.Helper()
	ctx := context.Background()
	var f opdsFixture

	ed := &models.Edition{Title: "Сочинения в 13 томах", Slug: "stalin-sochineniya", URLSlug: "stalin"}
	if err := repository.NewEditionRepository(pool).Create(ctx, ed); err != nil {
		t.Fatal(err)
	}
	f.edition = ed.ID

	works := repository.NewWorkRepository(pool)
	vol := &models.Work{
		Title: "И. В. Сталин. Сочинения. Том 1", Author: "И. В. Сталин", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleVolume, NumberingStyle: models.NumberingArabic,
		OwnerID: 1, EditionID: &f.edition, VolumeNumber: ptr(1), PageOffset: 2,
	}
	if err := works.Create(ctx, vol); err != nil {
		t.Fatal(err)
	}
	f.volume = vol.ID
	front := &models.Work{
		Title: "Передние листы", Author: "И. В. Сталин", Language: "ru", Country: "ru",
		Status: models.WorkStatusDraft, Role: models.WorkRoleFrontMatter, NumberingStyle: models.NumberingArabic,
		OwnerID: 1, ParentWorkID: &f.volume,
	}
	if err := works.Create(ctx, front); err != nil {
		t.Fatal(err)
	}
	f.front = front.ID

	pages := repository.NewPageRepository(pool)
	for i, md := range []string{
		"Письмо первое о кутаисских делах.", "Продолжение письма.",
		"Статья о разногласиях.", "Конец статьи.",
		"Анархизм или социализм — отдельная работа.",
		"1. Примечание к письму.",
	} {
		if err := pages.Create(ctx, &models.Page{WorkID: f.volume, PageNumber: i + 1,
			ContentMarkdown: md, Status: models.PageStatusNotProofread}); err != nil {
			t.Fatal(err)
		}
	}
	if err := pages.Create(ctx, &models.Page{WorkID: f.front, PageNumber: 1,
		ContentMarkdown: "Титул.", Status: models.PageStatusNotProofread}); err != nil {
		t.Fatal(err)
	}

	chapter := func(parent *int64, title, typ string, order, from, to int, apparatus bool) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO chapters (work_id, parent_id, title, type, order_number, start_page, end_page, is_apparatus)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			f.volume, parent, title, typ, order, from, to, apparatus).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.years = chapter(nil, "1901–1907", "part", 1, 1, 4, false)
	f.letter = chapter(&f.years, "Письмо из Кутаиса", "chapter", 2, 1, 2, false)
	f.essay = chapter(&f.years, "Коротко о партийных разногласиях", "chapter", 3, 3, 4, false)
	f.leaf = chapter(nil, "Анархизм или социализм?", "chapter", 4, 5, 5, false)
	f.notes = chapter(nil, "Примечания", "chapter", 5, 6, 6, true)
	return f
}

func ptr[T any](v T) *T { return &v }

// sweepRouter — настоящий роутер API с настоящими выгрузкой, страницами
// краулера и каталогом над одной базой. Остальные обработчики nil: обход их
// не зовёт.
func sweepRouter(t *testing.T, pool *pgxpool.Pool) (http.Handler, *api.DownloadSource, *api.OPDSSource) {
	t.Helper()
	works := repository.NewWorkRepository(pool)
	chapters := repository.NewChapterRepository(pool)
	pages := repository.NewPageRepository(pool)
	editions := repository.NewEditionRepository(pool)
	renderer := markdown.NewRenderer()

	download := api.NewDownloadSource(works, chapters, pages, editions,
		repository.NewCollectionRepository(pool), renderer, sweepBase)
	seoHandler := seo.NewHandler(&seo.Source{
		Works: works, Chapters: chapters, Pages: pages, Editions: editions,
		Books: api.NewSEOBookSource(download), Catalog: repository.NewSEORepository(pool),
		Renderer: renderer, BaseURL: sweepBase,
	})
	search := api.NewSearchHandler(repository.NewSearchRepository(pool))
	src := api.NewOPDSSource(editions, works, chapters, repository.NewOPDSRepository(pool), search)

	authService := auth.NewService(&config.JWTConfig{Secret: "sweep", Expiration: time.Hour,
		RefreshExpiration: time.Hour, ReaderExpiration: time.Hour})
	rt := api.NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		api.NewDownloadHandler(download), nil, nil, nil, nil, search, seoHandler, nil, nil, authService).
		WithOPDS(opds.NewHandler(src, sweepBase))
	return rt.Setup(), download, src
}

type sweepLink struct{ rel, href, typ string }

// feedLinks — все ссылки документа: link у ленты и записей, Url у
// описания поиска (шаблон с подставленным запросом).
func feedLinks(t *testing.T, body []byte) []sweepLink {
	t.Helper()
	var out []sweepLink
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("документ не разбирается: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		attr := map[string]string{}
		for _, a := range se.Attr {
			attr[a.Name.Local] = a.Value
		}
		switch se.Name.Local {
		case "link":
			out = append(out, sweepLink{attr["rel"], attr["href"], attr["type"]})
		case "Url":
			out = append(out, sweepLink{"search-template",
				strings.Replace(attr["template"], "{searchTerms}", url.QueryEscape("Кутаиса"), 1), attr["type"]})
		}
	}
}

func validateOPDS(t *testing.T, body []byte) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		return
	}
	if exec.Command("python3", "-c", "import lxml.etree").Run() != nil {
		return
	}
	_, file, _, _ := runtime.Caller(0)
	cmd := exec.Command("python3", filepath.Join(filepath.Dir(file), "..", "opds", "testdata", "schema", "validate.py"))
	cmd.Stdin = bytes.NewReader(body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("лента не проходит схему OPDS: %s\n%s", stderr.String(), body)
	}
}

// TestOPDSSweepEveryLinkIsServed идёт от /opds по всем ссылкам всех лент и
// требует, чтобы каждая отвечала 200 своего типа: ленты — лентой, файлы —
// настоящим /api/.../download, обложки — карточкой, страница в читальне —
// каноническим адресом краулера (200, а не 301). Ловит обе поломки сразу:
// ссылки нет и ссылку никто не обслуживает.
func TestOPDSSweepEveryLinkIsServed(t *testing.T) {
	pool := repository.SharedTestPool(t)
	f := seedOPDSFixture(t, pool)
	router, _, _ := sweepRouter(t, pool)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	type visit struct{ path, typ string }
	queue := []visit{{"/opds", ""}}
	seen := map[string]bool{}
	var books, feeds int
	titles := map[string]bool{}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		if seen[v.path] {
			continue
		}
		seen[v.path] = true
		rec := get(v.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", v.path, rec.Code, rec.Body.String())
			continue
		}
		ctype := rec.Header().Get("Content-Type")
		if v.typ != "" && !strings.HasPrefix(ctype, strings.SplitN(v.typ, ";", 2)[0]) {
			t.Errorf("%s: Content-Type %q, ссылка обещала %q", v.path, ctype, v.typ)
		}
		if !strings.HasPrefix(v.path, "/opds") {
			continue
		}
		feeds++
		body := rec.Body.Bytes()
		if strings.HasPrefix(ctype, "application/atom+xml") {
			validateOPDS(t, body)
			var parsed struct {
				Entries []struct {
					Title string `xml:"title"`
				} `xml:"entry"`
			}
			_ = xml.Unmarshal(body, &parsed)
			for _, e := range parsed.Entries {
				titles[e.Title] = true
			}
		}
		for _, l := range feedLinks(t, body) {
			u, err := url.Parse(l.href)
			if err != nil || u.Scheme+"://"+u.Host != sweepBase {
				t.Errorf("%s: ссылка %q не на читальню", v.path, l.href)
				continue
			}
			path := u.Path
			if u.RawQuery != "" {
				path += "?" + u.RawQuery
			}
			switch {
			case l.rel == "alternate" && l.typ == "text/html":
				// Адрес SPA; бэкенд знает его страницей краулера.
				queue = append(queue, visit{"/seo" + path, "text/html"})
			case strings.HasPrefix(u.Path, "/opds"):
				queue = append(queue, visit{path, l.typ})
			default:
				if strings.Contains(l.rel, "acquisition") {
					books++
				}
				queue = append(queue, visit{path, l.typ})
			}
		}
	}

	// Обход дошёл до каждого уровня неудобного тома.
	for _, want := range []string{
		"И. В. Сталин. Сочинения. Том 1 — целиком",
		"1901–1907", "1901–1907 — целиком",
		"Письмо из Кутаиса", "Коротко о партийных разногласиях", "Анархизм или социализм?",
	} {
		if !titles[want] {
			t.Errorf("обход не встретил запись %q", want)
		}
	}
	for _, hidden := range []string{"Примечания", "Передние листы"} {
		if titles[hidden] {
			t.Errorf("в каталоге видна запись %q", hidden)
		}
	}
	if books == 0 || feeds < 6 {
		t.Errorf("обход подозрительно мал: лент %d, файлов %d", feeds, books)
	}
	for _, path := range []string{
		"/opds/works/" + itoa(f.front),
		"/opds/works/" + itoa(f.volume) + "/chapters/" + itoa(f.notes),
	} {
		if rec := get(path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, ждали 404", path, rec.Code)
		}
	}
}

// TestOPDSUpdatedMatchesDownloadETag — метка updated записи обязана совпадать
// с CacheKey её файла: агрегатор видит перемену книги ровно тогда, когда
// меняется ETag выгрузки. Формулы живут в двух местах (download_source.go и
// OPDSSource), и разойтись им этот тест не даёт.
func TestOPDSUpdatedMatchesDownloadETag(t *testing.T) {
	pool := repository.SharedTestPool(t)
	f := seedOPDSFixture(t, pool)
	_, download, src := sweepRouter(t, pool)
	ctx := context.Background()

	// Правка заглавия главы двигает chapters.updated_at, но не полосы.
	time.Sleep(10 * time.Millisecond)
	if _, err := pool.Exec(ctx, `UPDATE chapters SET title = title || '.' WHERE id = $1`, f.essay); err != nil {
		t.Fatal(err)
	}

	tree, err := src.Tree(ctx, f.volume)
	if err != nil {
		t.Fatal(err)
	}
	b, err := download.Work(ctx, f.volume)
	if err != nil {
		t.Fatal(err)
	}
	if !tree.Volume.Updated.Equal(b.Meta.CacheKey) {
		t.Errorf("том: updated %v, CacheKey %v", tree.Volume.Updated, b.Meta.CacheKey)
	}
	var walk func([]*opds.Node)
	walk = func(nodes []*opds.Node) {
		for _, n := range nodes {
			if n.IsApparatus {
				continue
			}
			b, err := download.Chapter(ctx, f.volume, n.ID)
			if err != nil {
				t.Fatalf("глава %d: %v", n.ID, err)
			}
			if !n.Updated.Equal(b.Meta.CacheKey) {
				t.Errorf("глава %q: updated %v, CacheKey %v", n.Title, n.Updated, b.Meta.CacheKey)
			}
			walk(n.Children)
		}
	}
	walk(tree.Nodes)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
