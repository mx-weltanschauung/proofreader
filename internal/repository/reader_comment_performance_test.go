package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"proofreader/internal/models"
)

// Снимает настоящий запрос List, а не копию SQL из реализации.
type commentQueryTrace struct {
	sql  string
	args []any
}

func (tr *commentQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	tr.sql = data.SQL
	tr.args = append([]any(nil), data.Args...)
	return ctx
}
func (tr *commentQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestReaderCommentListAvoidsCorpusWidePlan(t *testing.T) {
	ctx, f, reader := marginSetup(t)
	pool := testPool(t)
	vol := newVolume(t, f.works, "План комментариев")
	if _, err := pool.Exec(ctx, `INSERT INTO pages(work_id,page_number,preview_path,content_markdown,status) SELECT $1,n,'','','не_вычитана' FROM generate_series(1,50000) n`, vol.ID); err != nil {
		t.Fatal(err)
	}
	// У корпуса много глав, но комментарий касается только одной полосы.
	if _, err := pool.Exec(ctx, `INSERT INTO chapters(work_id,title,type,order_number,start_page,end_page,closed) SELECT $1,'Глава','chapter',n,n*10-9,n*10,n>2500 FROM generate_series(1,5000) n`, vol.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE chapters c SET parent_id=p.id FROM chapters p WHERE c.work_id=$1 AND p.work_id=$1 AND (c.order_number-1)%10<>0 AND p.order_number=((c.order_number-1)/10)*10+1`, vol.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE chapters SET end_page=start_page+99 WHERE work_id=$1 AND (order_number-1)%10=0`, vol.ID); err != nil {
		t.Fatal(err)
	}
	p, err := f.pages.GetByWorkAndPageNumber(ctx, vol.ID, 15)
	if err != nil {
		t.Fatal(err)
	}
	staff := models.CommentViewer{ID: 1, Staff: true, Admin: true}
	repo := NewReaderCommentRepository(pool)
	in := quoteInput(reader.ID, vol.ID, p, "опубликованное")
	if _, err := repo.Save(ctx, staff, 0, in, "ip", 0, 0); err != nil {
		t.Fatal(err)
	}
	// Много закрытых поддеревьев воспроизводит ту же оценку, что и колонка
	// closed без статистики: Postgres предполагает, что закрыта половина глав.
	if _, err := pool.Exec(ctx, `ANALYZE pages; ANALYZE chapters; ANALYZE reader_comments`); err != nil {
		t.Fatal(err)
	}
	trace := &commentQueryTrace{}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer traced.Close()
	list, err := NewReaderCommentRepository(traced).List(ctx, models.CommentViewer{}, vol.ID, true, false, false, 200, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v, строк %d", err, len(list))
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+trace.sql, trace.args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan []struct {
		Plan struct {
			TotalCost float64 `json:"Total Cost"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan) != 1 {
		t.Fatalf("неожиданный план: %s", raw)
	}
	t.Logf("Стоимость плана одной цитаты: %.0f", plan[0].Plan.TotalCost)
	// Одна цитата не должна порождать план уровня всего корпуса и включать
	// дорогостоящую компиляцию JIT (стандартный jit_above_cost = 100000).
	if plan[0].Plan.TotalCost >= 100000 {
		t.Fatalf("план одной цитаты провоцирует JIT: стоимость %.0f", plan[0].Plan.TotalCost)
	}
}
