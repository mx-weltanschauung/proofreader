package repository_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"proofreader/internal/api"
	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/limit"
	"proofreader/internal/mcp"
	"proofreader/internal/models"
	"proofreader/internal/opds"
	"proofreader/internal/repository"
	"proofreader/internal/seo"
	"proofreader/pkg/markdown"
)

// tokenlessRouter — каналы без токена, собранные так же, как в
// cmd/server/main.go: краулер, тексты для нейросетей, OPDS и MCP — над
// открытым видом работ и изданий, выгрузка — двумя источниками, маршруты тома
// — за closedGate.
//
// Третий результат — сброс кэша краулера: закрытие через API сбрасывает его
// само (ClosedHandler), тест закрывает репозиторием и сбрасывает руками.
func tokenlessRouter(t *testing.T, pool *pgxpool.Pool) (http.Handler, http.Handler, func()) {
	t.Helper()
	works := repository.NewWorkRepository(pool)
	chapters := repository.NewChapterRepository(pool)
	pages := repository.NewPageRepository(pool)
	editions := repository.NewEditionRepository(pool)
	collections := repository.NewCollectionRepository(pool)
	closed := repository.NewClosedRepository(pool)
	renderer := markdown.NewRenderer()

	full := api.NewDownloadSource(works, chapters, pages, editions, collections, renderer, sweepBase)
	openWorks := api.NewOpenWorks(works, closed)
	openEditions := api.NewOpenEditions(editions, closed)
	open := api.NewDownloadSource(openWorks, chapters, pages, openEditions, collections, renderer, sweepBase)
	searchRepo := repository.NewSearchRepository(pool)
	search := api.NewSearchHandler(searchRepo)
	seoHandler := seo.NewHandler(&seo.Source{
		Works: openWorks, Chapters: chapters, Pages: pages, Editions: openEditions,
		Books: api.NewSEOBookSource(open), Catalog: repository.NewSEORepository(pool),
		Renderer: renderer, BaseURL: sweepBase,
	})
	src := api.NewOPDSSource(openEditions, openWorks, chapters, repository.NewOPDSRepository(pool), search)

	authService := auth.NewService(&config.JWTConfig{Secret: "sweep", Expiration: time.Hour,
		RefreshExpiration: time.Hour, ReaderExpiration: time.Hour})
	rt := api.NewRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		api.NewDownloadHandler(full).WithOpen(open).WithClosed(closed), nil, nil, nil, nil, search, seoHandler, nil, nil, authService).
		WithOPDS(opds.NewHandler(src, sweepBase)).
		WithClosed(closed)

	mcpHandler := mcp.NewHandler(mcp.Deps{
		BaseURL: sweepBase, Search: searchRepo, SearchSlots: limit.New(2), Text: seoHandler,
		Works: openWorks, Chapters: chapters, Library: api.NewMCPSource(open),
	})
	return rt.Setup(), mcpHandler, func() { seoHandler.Purge() }
}

// TestClosedVolumeAbsentFromTokenlessChannels — закрытый том в каналах без
// токена не существует: страницы краулера и тексты для нейросетей отвечают
// 410 (как снятый), карточки — 404, в llms.txt, карте сайта и лентах OPDS его
// нет, MCP его текста не отдаёт, выгрузка анониму — 404. Каждая проверка
// сперва проходит на открытом томе: иначе зелёный прогон значил бы только,
// что адрес неверен.
func TestClosedVolumeAbsentFromTokenlessChannels(t *testing.T) {
	pool := repository.SharedTestPool(t)
	f := seedOPDSFixture(t, pool)
	router, mcpHandler, purge := tokenlessRouter(t, pool)
	ctx := context.Background()
	const title = "И. В. Сталин. Сочинения. Том 1"
	vol, ch := itoa(f.volume), itoa(f.letter)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	// pages — адреса, у которых меняется код ответа.
	pages := map[string]int{
		"/seo/works/" + vol:                                             http.StatusGone,
		"/seo/works/" + vol + "/chapters/" + ch:                         http.StatusGone,
		"/seo/works/" + vol + "/pages/1":                                http.StatusGone,
		"/seo/works/" + vol + ".md":                                     http.StatusGone,
		"/seo/works/" + vol + "/chapters/" + ch + ".md":                 http.StatusGone,
		"/og/work/" + vol + ".png":                                      http.StatusNotFound,
		"/og/chapter/" + ch + ".png":                                    http.StatusNotFound,
		"/opds/works/" + vol:                                            http.StatusNotFound,
		"/api/works/" + vol + "/download?format=md":                     http.StatusNotFound,
		"/api/works/" + vol + "/chapters/" + ch + "/download?format=md": http.StatusNotFound,
	}
	// lists — перечни, в которых тома не должно стать.
	lists := []string{"/seo/llms.txt", "/sitemap-works.xml", "/sitemap-chapters-1.xml",
		"/opds/editions/" + itoa(f.edition), "/opds/new",
		"/opds/search?q=" + url.QueryEscape("Кутаиса"),
		"/seo/editions/" + itoa(f.edition)}
	mentions := func(body string) bool {
		return strings.Contains(body, title) || strings.Contains(body, "/works/"+vol)
	}

	// Голый номер краулер переводит 301 на канон со слагом; закрытому обязаны
	// отказывать оба вида адреса.
	for path, want := range pages {
		rec := get(path)
		if rec.Code == http.StatusMovedPermanently {
			loc := canonical(path, rec)
			pages[loc] = want
			rec = get(loc)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("открытый том: %s отвечает %d — проверка ничего не значит", path, rec.Code)
		}
	}
	for _, path := range lists {
		rec := get(path)
		if rec.Code == http.StatusMovedPermanently {
			rec = get(canonical(path, rec))
		}
		if rec.Code != http.StatusOK || !mentions(rec.Body.String()) {
			t.Fatalf("открытый том: %s (%d) его не называет — проверка ничего не значит", path, rec.Code)
		}
	}
	cs := mcpClient(t, mcpHandler)
	mcpCalls := []map[string]any{
		{"id": "/works/" + vol},
		{"id": "/works/" + vol + "/chapters/" + ch},
		{"id": "/works/" + vol + "/pages/1-2"},
	}
	for _, args := range mcpCalls {
		if text, isErr := mcpFetch(t, cs, args); isErr || !strings.Contains(text, "Кутаис") && !strings.Contains(text, title) {
			t.Fatalf("открытый том: MCP fetch %v не отдал текста: %s", args, text)
		}
	}

	if _, err := repository.NewClosedRepository(pool).SetWorkClosed(ctx, f.volume, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
		t.Fatal(err)
	}
	purge()

	for path, want := range pages {
		if rec := get(path); rec.Code != want {
			t.Errorf("закрытый том: %s отвечает %d, ждали %d", path, rec.Code, want)
		}
	}
	for _, path := range lists {
		rec := get(path)
		if rec.Code == http.StatusMovedPermanently {
			rec = get(canonical(path, rec))
		}
		if mentions(rec.Body.String()) {
			t.Errorf("закрытый том назван в %s (%d)", path, rec.Code)
		}
	}
	for _, args := range mcpCalls {
		if text, isErr := mcpFetch(t, cs, args); !isErr || strings.Contains(text, "Кутаис") {
			t.Errorf("закрытый том: MCP fetch %v отдал текст: %s", args, text)
		}
	}
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "search_volume",
		Arguments: map[string]any{"query": "Кутаиса", "work": f.volume}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Errorf("закрытый том: MCP search_volume ответил выдачей: %+v", res.Content)
	}
}

// TestClosedEditionAbsentFromTokenlessChannels — закрытое собрание: страница
// краулера 410, карточка 404, в llms.txt, карте сайта и OPDS его нет.
func TestClosedEditionAbsentFromTokenlessChannels(t *testing.T) {
	pool := repository.SharedTestPool(t)
	f := seedOPDSFixture(t, pool)
	router, _, purge := tokenlessRouter(t, pool)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	const name = "Сочинения в 13 томах"
	ed := itoa(f.edition)

	edPage := "/seo/editions/" + ed
	if rec := get(edPage); rec.Code == http.StatusMovedPermanently {
		edPage = canonical(edPage, rec)
	}
	for _, path := range []string{edPage, "/og/edition/" + ed + ".png", "/seo/llms.txt", "/opds/editions"} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Fatalf("открытое собрание: %s отвечает %d", path, rec.Code)
		}
	}
	if _, err := repository.NewClosedRepository(pool).SetEditionClosed(context.Background(), f.edition, true, models.ClosureActor{Name: "test"}, "test"); err != nil {
		t.Fatal(err)
	}
	purge()
	for _, path := range []string{"/seo/editions/" + ed, edPage} {
		if rec := get(path); rec.Code != http.StatusGone {
			t.Errorf("закрытое собрание краулеру (%s): %d, ждали 410", path, rec.Code)
		}
	}
	if rec := get("/og/edition/" + ed + ".png"); rec.Code != http.StatusNotFound {
		t.Errorf("карточка закрытого собрания: %d, ждали 404", rec.Code)
	}
	if rec := get("/opds/editions/" + ed); rec.Code != http.StatusNotFound {
		t.Errorf("закрытое собрание в OPDS: %d, ждали 404", rec.Code)
	}
	for _, path := range []string{"/seo/llms.txt", "/sitemap-editions.xml", "/sitemap-works.xml", "/opds", "/opds/editions", "/opds/new"} {
		body := get(path).Body.String()
		if strings.Contains(body, name) || strings.Contains(body, "/editions/"+ed) ||
			strings.Contains(body, "/works/"+itoa(f.volume)) {
			t.Errorf("закрытое собрание или его том названы в %s", path)
		}
	}
}

// canonical — адрес, на который краулер перевёл 301, в том же пространстве,
// что запрошенный: канон печатается адресом читальни, без /seo.
func canonical(path string, rec *httptest.ResponseRecorder) string {
	loc := strings.TrimPrefix(rec.Header().Get("Location"), sweepBase)
	if strings.HasPrefix(path, "/seo") && !strings.HasPrefix(loc, "/seo") {
		loc = "/seo" + loc
	}
	return loc
}

func mcpClient(t *testing.T, h http.Handler) *sdk.ClientSession {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).
		Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func mcpFetch(t *testing.T, cs *sdk.ClientSession, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "fetch", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

// Закрытая глава в конце тома: каталог OPDS не называет её полос (диапазон
// страниц тома, updated, число полос) и не двигает метку правкой закрытой
// полосы — метка обязана совпадать с CacheKey выгрузки анониму, которая
// закрытых полос не видит (боевая проводка: ViewPages и ViewChapters).
func TestOPDSStampsIgnoreClosedChapterPages(t *testing.T) {
	pool := repository.SharedTestPool(t)
	f := seedOPDSFixture(t, pool)
	ctx := context.Background()
	if err := repository.NewPageRepository(pool).Create(ctx, &models.Page{WorkID: f.volume, PageNumber: 7,
		ContentMarkdown: "Закрытый хвост.", Status: models.PageStatusNotProofread}); err != nil {
		t.Fatal(err)
	}
	var closedCh int64
	if err := pool.QueryRow(ctx, `INSERT INTO chapters (work_id, title, type, order_number, start_page, end_page, closed)
		VALUES ($1, 'Закрытый хвост', 'chapter', 6, 7, 7, true) RETURNING id`, f.volume).Scan(&closedCh); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE pages SET updated_at = now() + interval '1 day' WHERE work_id = $1 AND page_number = 7`, f.volume); err != nil {
		t.Fatal(err)
	}

	works := repository.NewWorkRepository(pool)
	closed := repository.NewClosedRepository(pool)
	viewChapters := api.NewViewChapters(repository.NewChapterRepository(pool), closed)
	viewPages := api.NewViewPages(repository.NewPageRepository(pool), closed)
	editions := repository.NewEditionRepository(pool)
	download := api.NewDownloadSource(works, viewChapters, viewPages, editions,
		repository.NewCollectionRepository(pool), markdown.NewRenderer(), sweepBase)
	src := api.NewOPDSSource(editions, works, viewChapters, repository.NewOPDSRepository(pool),
		api.NewSearchHandler(repository.NewSearchRepository(pool)))

	tree, err := src.Tree(ctx, f.volume)
	if err != nil {
		t.Fatal(err)
	}
	b, err := download.Work(ctx, f.volume)
	if err != nil {
		t.Fatal(err)
	}
	if !tree.Volume.Updated.Equal(b.Meta.CacheKey) {
		t.Errorf("updated тома %v, CacheKey выгрузки %v: закрытая полоса двинула метку", tree.Volume.Updated, b.Meta.CacheKey)
	}
	if tree.Volume.PageTo != 6+2 { // полосы 1—6, смещение тома 2
		t.Errorf("диапазон тома заканчивается на с. %d: закрытая полоса 7 в нём", tree.Volume.PageTo)
	}
	for _, n := range tree.Nodes {
		if n.ID == closedCh {
			t.Errorf("закрытая глава в дереве: %+v", n)
		}
	}
}
