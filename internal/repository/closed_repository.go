package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// ErrClosedTargetNotFound — у признака «закрыт» нет цели: работы, издания или
// журнала с таким id не существует.
var ErrClosedTargetNotFound = errors.New("closed target not found")

// ClosedRepository хранит признак «закрыт» у работы, издания и журнала.
// Действующую закрытость работы считает SQL-функция work_closed_by (причина:
// work | parent | edition | journal, пусто — открыта); представление
// closed_works — те же работы одним множеством.
type ClosedRepository struct {
	pool *pgxpool.Pool
}

func NewClosedRepository(pool *pgxpool.Pool) *ClosedRepository {
	return &ClosedRepository{pool: pool}
}

// closedByJoins и closedByCase — work_closed_by соединениями, а не вызовом
// функции на строку: в списках (полка, сводки издания, worksWithSlug) вызов
// стоил ~35 мкс на работу — главная из ~35 мс становилась ~55. Правило то же
// и в том же порядке причин (work > parent > edition > journal); совпадение
// с функцией на всех путях сторожит TestClosedBySetMatchesFunction.
//
// Работа зовётся works — как в worksWithSlug; запросам с алиасом w префикс
// переписывается подстановкой (closedByJoinsW), тем же приёмом, что у
// workSummariesSlugColumns. Алиасы cb_* соединения заводят сами; номер
// журнала у работы один (journal_issues_work_id_key), строки не множатся.
const closedByJoins = `
	LEFT JOIN works cb_p ON cb_p.id = works.parent_work_id
	LEFT JOIN editions cb_pe ON cb_pe.id = cb_p.edition_id
	LEFT JOIN editions cb_e ON cb_e.id = works.edition_id
	LEFT JOIN journal_issues cb_ji ON cb_ji.work_id = works.id
	LEFT JOIN journals cb_j ON cb_j.id = cb_ji.journal_id`

// closedByCase — причина закрытости над соединениями closedByJoins. NULL у
// отсутствующего родителя, издания или журнала в WHEN — не истина, поэтому
// coalesce не нужен.
const closedByCase = `CASE
	    WHEN works.closed THEN 'work'
	    WHEN cb_p.closed OR cb_pe.closed THEN 'parent'
	    WHEN cb_e.closed THEN 'edition'
	    WHEN cb_j.closed THEN 'journal'
	    ELSE ''
	END`

var (
	closedByJoinsW = strings.ReplaceAll(closedByJoins, "works.", "w.")
	closedByCaseW  = strings.ReplaceAll(closedByCase, "works.", "w.")
)

// ClosedBy — причина действующей закрытости работы; пусто — открыта. Нет
// такой работы — тоже пусто и без ошибки: закрытым то, чего нет, не бывает.
func (r *ClosedRepository) ClosedBy(ctx context.Context, workID int64) (string, error) {
	var by *string
	if err := r.pool.QueryRow(ctx, `SELECT work_closed_by($1)`, workID).Scan(&by); err != nil {
		return "", fmt.Errorf("work_closed_by: %w", err)
	}
	if by == nil {
		return "", nil
	}
	return *by, nil
}

// ClosedIDs — все работы, закрытые действующе (любой из четырёх причин).
func (r *ClosedRepository) ClosedIDs(ctx context.Context) (map[int64]bool, error) {
	return r.idSet(ctx, `SELECT id FROM closed_works`)
}

// ClosedEditionIDs — закрытые издания.
func (r *ClosedRepository) ClosedEditionIDs(ctx context.Context) (map[int64]bool, error) {
	return r.idSet(ctx, `SELECT id FROM editions WHERE closed`)
}

// WorksWithClosedChapters — работы, у которых закрыта хоть одна глава: их
// оригинал (PDF/DJVU) не-сотруднику не выдаётся.
func (r *ClosedRepository) WorksWithClosedChapters(ctx context.Context) (map[int64]bool, error) {
	return r.idSet(ctx, `SELECT DISTINCT work_id FROM closed_chapters`)
}

func (r *ClosedRepository) idSet(ctx context.Context, query string) (map[int64]bool, error) {
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("closed ids: %w", err)
	}
	defer rows.Close()
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// Виды сущностей в журнале закрытий (closures.entity_kind).
const (
	ClosureWork    = "work"
	ClosureEdition = "edition"
	ClosureJournal = "journal"
	ClosureChapter = "chapter"
)

// SetWorkClosed меняет собственный признак работы и возвращает id работ,
// чья действующая закрытость могла смениться: саму работу и её служебные.
// Признак и строка журнала — одной транзакцией; пустая причина откатывает обе.
func (r *ClosedRepository) SetWorkClosed(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.set(ctx, ClosureWork, `UPDATE works SET closed = $2 WHERE id = $1 RETURNING title`,
		`SELECT id FROM works WHERE id = $1 OR parent_work_id = $1 ORDER BY id`, id, closed, actor, reason)
}

// SetEditionClosed — то же для издания: возвращает все его работы и их
// служебные.
func (r *ClosedRepository) SetEditionClosed(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.set(ctx, ClosureEdition, `UPDATE editions SET closed = $2 WHERE id = $1 RETURNING title`,
		`SELECT w.id FROM works w
		 WHERE w.edition_id = $1
		    OR w.parent_work_id IN (SELECT id FROM works WHERE edition_id = $1)
		 ORDER BY w.id`, id, closed, actor, reason)
}

// SetJournalClosed — то же для журнала: возвращает работы его номеров.
func (r *ClosedRepository) SetJournalClosed(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.set(ctx, ClosureJournal, `UPDATE journals SET closed = $2 WHERE id = $1 RETURNING title`,
		`SELECT work_id FROM journal_issues WHERE journal_id = $1 ORDER BY work_id`, id, closed, actor, reason)
}

// SetChapterClosed — то же для главы: возвращает работу главы (закрытость
// потомков и полос выводится представлениями, других работ она не задевает).
func (r *ClosedRepository) SetChapterClosed(ctx context.Context, id int64, closed bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.set(ctx, ClosureChapter, `UPDATE chapters SET closed = $2 WHERE id = $1 RETURNING title`,
		`SELECT work_id FROM chapters WHERE id = $1`, id, closed, actor, reason)
}

// set пишет признак, строку журнала и собирает затронутые работы одной
// транзакцией. Нет цели — ErrClosedTargetNotFound, ничего не записано. Повтор
// с тем же значением безвреден, тоже пишет строку и возвращает те же работы:
// вызывающему нужно сбросить по ним кэши.
func (r *ClosedRepository) set(ctx context.Context, kind, update, affected string, id int64, closed bool,
	actor models.ClosureActor, reason string) ([]int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var title string
	if err := tx.QueryRow(ctx, update, id, closed).Scan(&title); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClosedTargetNotFound
		}
		return nil, fmt.Errorf("set closed: %w", err)
	}
	if err := insertClosureTx(ctx, tx, kind, id, title, closed, actor, reason); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, affected, id)
	if err != nil {
		return nil, fmt.Errorf("closed affected works: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var w int64
		if err := rows.Scan(&w); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}

// insertClosureTx пишет строку журнала в чужой транзакции — её зовут и
// Set…Closed, и Create закрытой сущности. Пустая причина отвергается CHECK
// таблицы, то есть откатывает всю транзакцию вызывающего.
func insertClosureTx(ctx context.Context, tx pgx.Tx, kind string, id int64, title string, closed bool,
	actor models.ClosureActor, reason string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO closures
		(entity_kind, entity_id, title_snapshot, closed, reason, actor_id, actor_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		kind, id, title, closed, reason, actor.ID, actor.Name); err != nil {
		return fmt.Errorf("closures: %w", err)
	}
	return nil
}

// ClosedSpans — диапазоны полос закрытых глав работы (сами закрытые и их
// потомки), по началу диапазона.
func (r *ClosedRepository) ClosedSpans(ctx context.Context, workID int64) ([]models.PageSpan, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT start_page, end_page FROM closed_chapters WHERE work_id = $1 ORDER BY start_page, end_page`, workID)
	if err != nil {
		return nil, fmt.Errorf("closed spans: %w", err)
	}
	defer rows.Close()
	spans := []models.PageSpan{}
	for rows.Next() {
		var s models.PageSpan
		if err := rows.Scan(&s.Start, &s.End); err != nil {
			return nil, err
		}
		spans = append(spans, s)
	}
	return spans, rows.Err()
}

// ClosedChapterIDs — главы работы, закрытые действующе (своим признаком или
// закрытым предком).
func (r *ClosedRepository) ClosedChapterIDs(ctx context.Context, workID int64) (map[int64]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM closed_chapters WHERE work_id = $1`, workID)
	if err != nil {
		return nil, fmt.Errorf("closed chapter ids: %w", err)
	}
	defer rows.Close()
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

const closureColumns = `entity_kind, entity_id, title_snapshot, closed, reason, actor_name, created_at`

func scanClosure(row rowScanner) (*models.Closure, error) {
	var c models.Closure
	if err := row.Scan(&c.Kind, &c.EntityID, &c.Title, &c.Closed, &c.Reason, &c.ActorName, &c.At); err != nil {
		return nil, err
	}
	return &c, nil
}

// LatestClosure — последняя строка журнала по сущности; nil, nil — строк нет.
func (r *ClosedRepository) LatestClosure(ctx context.Context, kind string, id int64) (*models.Closure, error) {
	c, err := scanClosure(r.pool.QueryRow(ctx, `SELECT `+closureColumns+` FROM closures
		WHERE entity_kind = $1 AND entity_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1`, kind, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest closure: %w", err)
	}
	return c, nil
}

// ListClosures — журнал закрытий, новые сверху.
func (r *ClosedRepository) ListClosures(ctx context.Context, limit, offset int) ([]models.Closure, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+closureColumns+` FROM closures
		ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list closures: %w", err)
	}
	defer rows.Close()
	out := []models.Closure{}
	for rows.Next() {
		c, err := scanClosure(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
