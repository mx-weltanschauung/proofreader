package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/repository"
	"proofreader/internal/staticsite"
)

// policy — что идёт в архив, сведённое из -exclude и -only. Одно на build и
// check: самопроверка обязана судить ровно тот состав, что собран.
type policy struct {
	// exclude — снятые работы вместе с их передними листами.
	exclude map[int64]bool
	// only — каталог боевого вместе с передними листами; nil — все работы.
	only map[int64]bool
	// apparatus — работы со снятым аппаратом: внутренние номера его полос.
	apparatus map[int64]map[int]bool
	// apparatusFront — передние листы работ со снятым аппаратом: их снятие
	// аппарата уносит тоже.
	apparatusFront []int64
}

func loadPolicy(ctx context.Context, pool *pgxpool.Pool, excludeFile, onlyFile string) (*policy, error) {
	list, err := readExclude(excludeFile)
	if err != nil {
		return nil, err
	}
	p := &policy{exclude: map[int64]bool{}, apparatus: map[int64]map[int]bool{}}

	works := sortedKeys(list.Works)
	kids, err := childrenOf(ctx, pool, works)
	if err != nil {
		return nil, err
	}
	for _, id := range append(works, kids...) {
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

	if onlyFile == "" {
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
	return p, nil
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
