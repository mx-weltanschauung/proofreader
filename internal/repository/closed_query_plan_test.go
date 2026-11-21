package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These checks guard the query shapes that caused a full corpus walk and
// PostgreSQL JIT compilation on ordinary page and chapter reads.
func explainClosedQuery(t *testing.T, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(context.Background(), "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func TestScansHiddenWorksDoesNotCallFunctionPerWork(t *testing.T) {
	pool := testPool(t)
	plan := explainClosedQuery(t, pool, `SELECT id FROM scans_hidden_works`)
	if strings.Contains(plan, "work_scans_hidden_by(") {
		t.Fatalf("bulk scan restriction query calls the per-work function:\n%s", plan)
	}
}

func TestClosedPagesDoesNotCheckChapterSpansPerPage(t *testing.T) {
	pool := testPool(t)
	w := newVolume(t, NewWorkRepository(pool), "plan fixture")
	newPages(t, pool, w.ID, 1, 20)
	// Closed editions can legitimately introduce subplans on works. Only a
	// correlated subplan in the pages scan filter repeats for each page.
	type node struct {
		Relation string `json:"Relation Name"`
		Filter   string
		Plans    []node
	}
	var raw []byte
	err := pool.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) SELECT id FROM closed_pages WHERE work_id=$1", w.ID).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	var roots []struct{ Plan node }
	if err := json.Unmarshal(raw, &roots); err != nil || len(roots) != 1 {
		t.Fatalf("unexpected EXPLAIN: %s (%v)", raw, err)
	}
	var visit func(node)
	visit = func(n node) {
		if n.Relation == "pages" && strings.Contains(n.Filter, "OR (SubPlan") {
			t.Fatalf("page scan uses correlated subplan: %s", n.Filter)
		}
		for _, child := range n.Plans {
			visit(child)
		}
	}
	visit(roots[0].Plan)
}
