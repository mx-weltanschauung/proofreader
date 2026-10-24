package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/internal/staticsite"
)

// policy — что идёт в архив, сведённое из -exclude и -only. Одно на build и
// check: самопроверка обязана судить ровно тот состав, что собран.
type policy struct {
	// exclude — снятые работы вместе с их передними листами и закрытые.
	exclude map[int64]bool
	// only — каталог боевого вместе с передними листами; nil — все работы.
	only map[int64]bool
	// apparatus — работы со снятым аппаратом: внутренние номера его полос.
	apparatus map[int64]map[int]bool
	// apparatusFront — передние листы работ со снятым аппаратом: их снятие
	// аппарата уносит тоже.
	apparatusFront []int64
	// closedPages — внутренние номера полос, которых в архиве быть не должно:
	// полосы закрытых глав локальной базы (closed_pages) и полосы, которых
	// нет на боевом (-only-pages: там их скрывает закрытая глава, а у нас она
	// может быть ещё открыта). Снятый аппарат живёт отдельно, в apparatus.
	closedPages map[int64]map[int]bool
}

func loadPolicy(ctx context.Context, pool *pgxpool.Pool, excludeFile, onlyFile, onlyPagesFile string) (*policy, error) {
	list, err := readExclude(excludeFile)
	if err != nil {
		return nil, err
	}
	p := &policy{exclude: map[int64]bool{}, apparatus: map[int64]map[int]bool{}, closedPages: map[int64]map[int]bool{}}

	works := sortedKeys(list.Works)
	kids, err := childrenOf(ctx, pool, works)
	if err != nil {
		return nil, err
	}
	for _, id := range append(works, kids...) {
		p.exclude[id] = true
	}
	// Закрытые работы — по признаку локальной базы, независимо от списка и
	// от -only: архив раздают всем, а закрытое не видит никто, кроме
	// сотрудника. closed_works уже включает служебные работы закрытого тома
	// и тома закрытого издания. Попав в exclude, работа и сборкой
	// пропускается, и самопроверкой (checkExcluded) роняет выпуск.
	closed, err := closedWorks(ctx, pool)
	if err != nil {
		return nil, err
	}
	for _, id := range closed {
		p.exclude[id] = true
	}

	app := repository.NewApparatusRepository(pool)
	for _, id := range sortedKeys(list.Apparatus) {
		if p.exclude[id] {
			continue // том снят целиком — аппарат вместе с ним
		}
		nums, err := app.PageNumbers(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("аппарат работы %d: %w", id, err)
		}
		set := map[int]bool{}
		for _, n := range nums {
			set[n] = true
		}
		p.apparatus[id] = set
	}
	if p.apparatusFront, err = childrenOf(ctx, pool, sortedKeys(p.apparatus)); err != nil {
		return nil, err
	}

	// Закрытые главы локальной базы: контекст сборки без claims их полос не
	// видит, но самопроверка должна знать, что их в архиве быть не может.
	if err := p.addLocalClosedPages(ctx, pool); err != nil {
		return nil, err
	}

	if onlyFile == "" {
		if onlyPagesFile != "" {
			return nil, errors.New("-only-pages без -only: не к чему привязать карту полос боевого")
		}
		return p, nil
	}
	f, err := os.Open(onlyFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	only, err := staticsite.ParseOnlyList(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", onlyFile, err)
	}
	ids := make([]int64, 0, len(only))
	for _, w := range only {
		ids = append(ids, w.ID)
	}
	local, err := localTitles(ctx, pool, ids)
	if err != nil {
		return nil, err
	}
	if err := staticsite.CheckOnlyList(only, local); err != nil {
		return nil, err
	}
	kids, err = childrenOf(ctx, pool, ids)
	if err != nil {
		return nil, err
	}
	p.only = map[int64]bool{}
	for _, id := range append(ids, kids...) {
		p.only[id] = true
	}
	if onlyPagesFile == "" {
		return p, nil
	}
	if err := p.addMissingOnProd(ctx, pool, onlyPagesFile, only, kids); err != nil {
		return nil, err
	}
	return p, nil
}

// addLocalClosedPages кладёт в closedPages полосы закрытых глав локальной
// базы. Полосы закрытых работ не берутся: работа и так в exclude целиком.
func (p *policy) addLocalClosedPages(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT cp.work_id, cp.page_number FROM closed_pages cp
		WHERE cp.work_id NOT IN (SELECT id FROM closed_works) ORDER BY 1, 2`)
	if err != nil {
		return fmt.Errorf("закрытые полосы: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var work int64
		var n int
		if err := rows.Scan(&work, &n); err != nil {
			return err
		}
		if p.closedPages[work] == nil {
			p.closedPages[work] = map[int]bool{}
		}
		p.closedPages[work][n] = true
	}
	return rows.Err()
}

// addMissingOnProd читает -only-pages (GET /api/works/{id}/page-map боевого,
// собранный static-release.sh) и кладёт в closedPages локальные полосы
// работ каталога и их служебных работ, которых на боевом нет. Работа каталога
// без записи в файле —
// отказ: молча считать «все полосы есть» значило бы выпустить архив, который
// опережает сайт.
func (p *policy) addMissingOnProd(ctx context.Context, pool *pgxpool.Pool, file string, only []staticsite.OnlyWork, kids []int64) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	prod, err := staticsite.ParseProdPages(f)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	ids := make([]int64, 0, len(only))
	for _, w := range only {
		if p.exclude[w.ID] {
			continue // снята — в архив не идёт, сверять нечего
		}
		if _, ok := prod[w.ID]; !ok {
			return fmt.Errorf("%s: нет полос боевого у работы %d из -only", file, w.ID)
		}
		ids = append(ids, w.ID)
	}
	// Служебные работы (передние листы) в GET /api/works не входят: их карту
	// static-release.sh спрашивает отдельно, и ответ 404 (закрыта на боевом
	// или не выложена) в файл не попадает. Такая работа из архива уходит
	// целиком — как исключённая, а не как «все полосы на месте».
	for _, id := range kids {
		if p.exclude[id] {
			continue
		}
		if _, ok := prod[id]; !ok {
			delete(p.only, id)
			p.exclude[id] = true
			continue
		}
		ids = append(ids, id)
	}
	rows, err := pool.Query(ctx, `SELECT work_id, page_number FROM pages WHERE work_id = ANY($1) ORDER BY 1, 2`, ids)
	if err != nil {
		return fmt.Errorf("полосы базы: %w", err)
	}
	defer rows.Close()
	local := map[int64][]int{}
	for rows.Next() {
		var work int64
		var n int
		if err := rows.Scan(&work, &n); err != nil {
			return err
		}
		local[work] = append(local[work], n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for work, nums := range missingOnProd(local, prod) {
		if p.closedPages[work] == nil {
			p.closedPages[work] = map[int]bool{}
		}
		for n := range nums {
			p.closedPages[work][n] = true
		}
	}
	return nil
}

// missingOnProd — локальные полосы работ, которых нет в карте боевого. Работа
// без расхождений в результат не входит.
func missingOnProd(local, prod map[int64][]int) map[int64]map[int]bool {
	out := map[int64]map[int]bool{}
	for work, nums := range local {
		live := map[int]bool{}
		for _, n := range prod[work] {
			live[n] = true
		}
		for _, n := range nums {
			if live[n] {
				continue
			}
			if out[work] == nil {
				out[work] = map[int]bool{}
			}
			out[work][n] = true
		}
	}
	return out
}

// pageSpans сворачивает набор номеров в отсортированные диапазоны.
func pageSpans(set map[int]bool) []models.PageSpan {
	nums := make([]int, 0, len(set))
	for n := range set {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	var out []models.PageSpan
	for _, n := range nums {
		if k := len(out) - 1; k >= 0 && out[k].End+1 == n {
			out[k].End = n
			continue
		}
		out = append(out, models.PageSpan{Start: n, End: n})
	}
	return out
}

// excludedIDs — работы, которых в сборке быть не должно вовсе: снятые и
// передние листы томов со снятым аппаратом.
func (p *policy) excludedIDs() []int64 {
	return append(sortedKeys(p.exclude), p.apparatusFront...)
}

// onlyIDs — каталог боевого с передними листами; nil — без ограничения.
func (p *policy) onlyIDs() []int64 {
	if p.only == nil {
		return nil
	}
	return sortedKeys(p.only)
}

// closedWorks — работы, закрытые любым путём.
func closedWorks(ctx context.Context, pool *pgxpool.Pool) ([]int64, error) {
	rows, err := pool.Query(ctx, `SELECT id FROM closed_works ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("закрытые работы: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// childrenOf — служебные работы (передние листы) данных томов.
func childrenOf(ctx context.Context, pool *pgxpool.Pool, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `SELECT id FROM works WHERE parent_work_id = ANY($1) ORDER BY id`, ids)
	if err != nil {
		return nil, fmt.Errorf("передние листы: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// localTitles — заглавия работ локальной базы по номерам.
func localTitles(ctx context.Context, pool *pgxpool.Pool, ids []int64) (map[int64]string, error) {
	rows, err := pool.Query(ctx, `SELECT id, title FROM works WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("заглавия работ: %w", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		out[id] = title
	}
	return out, rows.Err()
}

// apparatusPrinted — печатные номера полос снятого аппарата каждой работы
// (внутренний номер + page_offset): так их видит самопроверка в якорях p<n>.
func apparatusPrinted(ctx context.Context, pool *pgxpool.Pool, apparatus map[int64]map[int]bool) (map[int64][]int, error) {
	out := map[int64][]int{}
	for id, set := range apparatus {
		var offset int
		if err := pool.QueryRow(ctx, `SELECT page_offset FROM works WHERE id = $1`, id).Scan(&offset); err != nil {
			return nil, fmt.Errorf("смещение полос работы %d: %w", id, err)
		}
		for n := range set {
			out[id] = append(out[id], n+offset)
		}
		slices.Sort(out[id])
	}
	return out, nil
}

// prodPrinted — печатные номера полос боевого (-prod-pages) у работ со снятым
// аппаратом: самопроверка сверяет с ними сборку независимо от локальной
// разметки is_apparatus. Работа «N apparatus» без своих полос боевого в файле
// — отказ, а не пропуск: иначе независимая сверка молча не случилась бы.
func prodPrinted(ctx context.Context, pool *pgxpool.Pool, file string, apparatus map[int64]map[int]bool) (map[int64][]int, error) {
	if len(apparatus) == 0 {
		return nil, nil
	}
	if file == "" {
		return nil, fmt.Errorf("есть работы со снятым аппаратом (%v) — нужен -prod-pages с их полосами на боевом", sortedKeys(apparatus))
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pages, err := staticsite.ParseProdPages(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	out := map[int64][]int{}
	for _, id := range sortedKeys(apparatus) {
		nums, ok := pages[id]
		if !ok {
			return nil, fmt.Errorf("%s: нет полос боевого у работы %d со снятым аппаратом", file, id)
		}
		var offset int
		if err := pool.QueryRow(ctx, `SELECT page_offset FROM works WHERE id = $1`, id).Scan(&offset); err != nil {
			return nil, fmt.Errorf("смещение полос работы %d: %w", id, err)
		}
		for _, n := range nums {
			out[id] = append(out[id], n+offset)
		}
		if out[id] == nil {
			out[id] = []int{}
		}
	}
	return out, nil
}

// writeBuilt пишет состав сборки (-built): из него static-release.sh
// собирает chitalnya-<дата>.build.json.
func writeBuilt(path string, g *staticsite.Generator) error {
	catalog, all := g.Built()
	data, err := json.Marshal(struct {
		CatalogIDs []int64 `json:"catalog_ids"`
		WorkIDs    []int64 `json:"work_ids"`
	}{catalog, all})
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func sortedKeys[V any](m map[int64]V) []int64 {
	out := make([]int64, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// printChildren печатает номера служебных работ (передних листов) работ из
// -only, по одному в строке: static-release.sh спрашивает боевой о каждой.
func printChildren(ctx context.Context, pool *pgxpool.Pool, onlyFile string) error {
	f, err := os.Open(onlyFile)
	if err != nil {
		return err
	}
	defer f.Close()
	only, err := staticsite.ParseOnlyList(f)
	if err != nil {
		return fmt.Errorf("%s: %w", onlyFile, err)
	}
	ids := make([]int64, 0, len(only))
	for _, w := range only {
		ids = append(ids, w.ID)
	}
	kids, err := childrenOf(ctx, pool, ids)
	if err != nil {
		return err
	}
	for _, id := range kids {
		fmt.Println(id)
	}
	return nil
}
