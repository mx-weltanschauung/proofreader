// staticsite — статическая читальня: весь текст корпуса папкой файлов.
// Спека: docs/superpowers/specs/2026-10-06-static-reading-room-design.md.
//
//	go run ./cmd/staticsite build -out static/chitalnya-2026-10-06.partial [-edition 7] [-date 2026-10-06]
//	    [-exclude scripts/static-exclude.txt] [-only works.json] [-built built.json]
//	go run ./cmd/staticsite check [-edition 7] [-exclude …] [-only …] static/chitalnya-2026-10-06.partial
//
// -only — ответ GET /api/works боевого: в архив идут только его работы
// (спека docs/superpowers/specs/2026-10-07-static-reading-room-publish-design.md).
//
// Целиком (pagefind, serve, zip) — scripts/static-release.sh.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"proofreader/internal/site"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"proofreader/internal/api"
	"proofreader/internal/config"
	"proofreader/internal/database"
	"proofreader/internal/repository"
	"proofreader/internal/staticsite"
	"proofreader/internal/staticsite/sitecheck"
	"proofreader/pkg/markdown"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: staticsite build -out DIR [-edition ID] [-date YYYY-MM-DD] [-base URL] [-exclude FILE] [-only FILE] [-built FILE]")
	fmt.Fprintln(os.Stderr, "       staticsite check [-edition ID] [-allow URL] [-exclude FILE] [-only FILE] DIR")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file loaded (%v); using environment variables", err)
	}
	// Имя и описание экземпляра — из того же .env, что у сервера.
	site.Set(os.Getenv("SITE_NAME"), os.Getenv("SITE_DESCRIPTION"))
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	db, err := database.New(&cfg.Database)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	switch os.Args[1] {
	case "build":
		err = build(ctx, db.Pool, os.Args[2:])
	case "check":
		err = check(ctx, db.Pool, os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		log.Fatal(err)
	}
}

func build(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("out", "", "каталог сборки: не существует или пуст")
	edition := fs.Int64("edition", 0, "только это издание (замер); 0 — весь корпус")
	date := fs.String("date", time.Now().UTC().Format("2006-01-02"), "дата сборки в подвале страниц")
	base := fs.String("base", os.Getenv("PUBLIC_BASE_URL"), "читальня онлайн — для ссылки «эта страница в читальне онлайн»")
	excludeFile := fs.String("exclude", "", "список работ, не идущих в архив (scripts/static-exclude.txt)")
	onlyFile := fs.String("only", "", "ответ GET /api/works боевого: в архив идут только его работы")
	builtFile := fs.String("built", "", "куда записать состав сборки (catalog_ids, work_ids)")
	_ = fs.Parse(args)
	if *out == "" {
		return errors.New("нужен -out")
	}
	pol, err := loadPolicy(ctx, pool, *excludeFile, *onlyFile)
	if err != nil {
		return err
	}

	works := repository.NewWorkRepository(pool)
	chapters := repository.NewChapterRepository(pool)
	pages := repository.NewPageRepository(pool)
	editions := repository.NewEditionRepository(pool)
	collections := repository.NewCollectionRepository(pool)
	index := repository.NewIndexRepository(pool)
	renderer := markdown.NewRenderer()

	books := api.NewDownloadSource(works, chapters, pages, editions, collections, renderer, *base).WithChapterAnchors()
	documents := api.NewSEODocumentSource(
		repository.NewDocumentRepository(pool), repository.NewDocumentCutRepository(pool), works, renderer)
	src := api.NewStaticSource(editions, works, books, pages, index, repository.NewSEORepository(pool),
		collections, documents, renderer)

	g := &staticsite.Generator{Src: src, Out: *out, BaseURL: *base, BuildDate: *date, EditionID: *edition,
		Exclude: pol.exclude, Only: pol.only, Apparatus: pol.apparatus, Logf: log.Printf}
	start := time.Now()
	if err := g.Run(ctx); err != nil {
		return err
	}
	log.Printf("сборка готова за %s: %s", time.Since(start).Round(time.Second), *out)
	if *builtFile != "" {
		return writeBuilt(*builtFile, g)
	}
	return nil
}

// expectedPagesQuery — непустые полосы каждой работы печатными номерами.
// ~ '\S', а не btrim: btrim снимает только пробелы, полоса из одних
// переводов строки прошла бы за непустую.
const expectedPagesQuery = `
	SELECT p.work_id, p.page_number + w.page_offset
	FROM pages p
	JOIN works w ON w.id = p.work_id
	WHERE p.content_markdown ~ '\S'
	  AND ($1::bigint = 0 OR w.edition_id = $1::bigint
	       OR w.parent_work_id IN (SELECT id FROM works WHERE edition_id = $1::bigint))
	  AND NOT (w.id = ANY($2::bigint[]) OR coalesce(w.parent_work_id = ANY($2::bigint[]), false))
	  AND ($3::bigint[] IS NULL OR w.id = ANY($3::bigint[]))
	ORDER BY 1, 2`

func check(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	edition := fs.Int64("edition", 0, "сборка одного издания (как у build)")
	allow := fs.String("allow", strings.TrimSuffix(os.Getenv("PUBLIC_BASE_URL"), "/")+"/", "допустимый префикс внешних ссылок <a href>")
	excludeFile := fs.String("exclude", "", "список работ, не идущих в архив (как у build)")
	onlyFile := fs.String("only", "", "ответ GET /api/works боевого (как у build)")
	prodPagesFile := fs.String("prod-pages", "", "полосы боевого у работ со снятым аппаратом: {\"id\": [номера]}")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("нужен каталог сборки")
	}
	pol, err := loadPolicy(ctx, pool, *excludeFile, *onlyFile)
	if err != nil {
		return err
	}
	excluded := pol.excludedIDs()

	rows, err := pool.Query(ctx, expectedPagesQuery, *edition, excluded, pol.onlyIDs())
	if err != nil {
		return fmt.Errorf("полосы базы: %w", err)
	}
	expected := map[int64][]int{}
	for rows.Next() {
		var work int64
		var printed int
		if err := rows.Scan(&work, &printed); err != nil {
			rows.Close()
			return err
		}
		expected[work] = append(expected[work], printed)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Полосы снятого аппарата полнота не требует — их сборке иметь нельзя.
	forbidden, err := apparatusPrinted(ctx, pool, pol.apparatus)
	if err != nil {
		return err
	}
	for id, nums := range forbidden {
		gone := map[int]bool{}
		for _, n := range nums {
			gone[n] = true
		}
		kept := expected[id][:0]
		for _, n := range expected[id] {
			if !gone[n] {
				kept = append(kept, n)
			}
		}
		expected[id] = kept
	}
	onProd, err := prodPrinted(ctx, pool, *prodPagesFile, pol.apparatus)
	if err != nil {
		return err
	}

	problems, err := sitecheck.Run(sitecheck.Options{Dir: fs.Arg(0), AllowedExternal: *allow, Expected: expected,
		Excluded: excluded, Allowed: pol.only, Forbidden: forbidden, OnProd: onProd})
	if err != nil {
		return err
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("самопроверка: %d расхождений", len(problems))
	}
	log.Printf("самопроверка: чисто (%d работ сверено с базой)", len(expected))
	return nil
}

// readExclude читает список работ, не идущих в архив; пустой путь — пустой
// список.
func readExclude(path string) (staticsite.ExcludeList, error) {
	if path == "" {
		return staticsite.ExcludeList{Works: map[int64]bool{}, Apparatus: map[int64]bool{}}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return staticsite.ExcludeList{}, err
	}
	defer f.Close()
	list, err := staticsite.ParseExcludeList(f)
	if err != nil {
		return staticsite.ExcludeList{}, fmt.Errorf("%s: %w", path, err)
	}
	return list, nil
}
