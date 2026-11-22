package repository

import (
	"context"
	"encoding/json"
	"regexp"
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

type closedPlanNode struct {
	Relation    string `json:"Relation Name"`
	Alias       string
	Subplan     string `json:"Subplan Name"`
	Filter      string
	JoinFilter  string `json:"Join Filter"`
	IndexCond   string `json:"Index Cond"`
	RecheckCond string `json:"Recheck Cond"`
	Plans       []closedPlanNode
}

func (n closedPlanNode) walk(visit func(closedPlanNode)) {
	visit(n)
	for _, child := range n.Plans {
		child.walk(visit)
	}
}

var pageNumberRef = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z_0-9]*)\.page_number\b`)

// A correlated range reads the page number from outside the subplan. The
// caller may evaluate it at a pages scan or after joining pages to works.
func hasCorrelatedChapterRange(plan closedPlanNode) bool {
	pages := map[string]bool{}
	var subplans []closedPlanNode
	plan.walk(func(n closedPlanNode) {
		if n.Relation == "pages" {
			pages[n.Alias] = true
		}
		if strings.HasPrefix(n.Subplan, "SubPlan ") {
			subplans = append(subplans, n)
		}
	})
	for _, sub := range subplans {
		localPages := map[string]bool{}
		var predicates strings.Builder
		sub.walk(func(n closedPlanNode) {
			if n.Relation == "pages" {
				localPages[n.Alias] = true
			}
			for _, cond := range []string{n.Filter, n.JoinFilter, n.IndexCond, n.RecheckCond} {
				predicates.WriteString(cond)
				predicates.WriteByte(' ')
			}
		})
		conditions := predicates.String()
		if !strings.Contains(conditions, "start_page") || !strings.Contains(conditions, "end_page") {
			continue
		}
		for _, ref := range pageNumberRef.FindAllStringSubmatch(conditions, -1) {
			if pages[ref[1]] && !localPages[ref[1]] {
				return true
			}
		}
	}
	return false
}

func TestCorrelatedChapterRangePlans(t *testing.T) {
	pageScan := closedPlanNode{Relation: "pages", Alias: "p"}
	span := closedPlanNode{Subplan: "SubPlan 9", Plans: []closedPlanNode{{Filter: "((p.page_number >= start_page) AND (p.page_number <= end_page))"}}}
	edition := closedPlanNode{Subplan: "SubPlan 3", Relation: "editions", Alias: "e", IndexCond: "(id = w.edition_id)", Filter: "closed"}
	for _, tc := range []struct {
		name string
		plan closedPlanNode
		want bool
	}{
		{"edition subplan", closedPlanNode{JoinFilter: "(w.closed OR (SubPlan 3))", Plans: []closedPlanNode{pageScan, edition}}, false},
		{"page filter", closedPlanNode{Relation: "pages", Alias: "p", Filter: "((hashed SubPlan 7) OR (SubPlan 9))", Plans: []closedPlanNode{span}}, true},
		{"join filter", closedPlanNode{JoinFilter: "(w.closed OR (SubPlan 9))", Plans: []closedPlanNode{pageScan, span}}, true},
		{"uncorrelated page range", closedPlanNode{Plans: []closedPlanNode{{Subplan: "SubPlan 2", JoinFilter: "((p.page_number >= c.start_page) AND (p.page_number <= c.end_page))", Plans: []closedPlanNode{pageScan}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCorrelatedChapterRange(tc.plan); got != tc.want {
				t.Errorf("correlated chapter range = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClosedPagesDoesNotCheckChapterSpansPerPage(t *testing.T) {
	pool := testPool(t)
	w := newVolume(t, NewWorkRepository(pool), "plan fixture")
	newPages(t, pool, w.ID, 1, 20)
	var raw []byte
	err := pool.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) SELECT id FROM closed_pages WHERE work_id=$1", w.ID).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	var roots []struct{ Plan closedPlanNode }
	if err := json.Unmarshal(raw, &roots); err != nil || len(roots) != 1 {
		t.Fatalf("unexpected EXPLAIN: %s (%v)", raw, err)
	}
	if hasCorrelatedChapterRange(roots[0].Plan) {
		t.Fatalf("page query checks closed chapter ranges once per page:\n%s", raw)
	}
}
