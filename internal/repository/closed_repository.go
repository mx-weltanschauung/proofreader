package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// SetWorkClosed меняет собственный признак работы и возвращает id работ,
// чья действующая закрытость могла смениться: саму работу и её служебные.
func (r *ClosedRepository) SetWorkClosed(ctx context.Context, id int64, closed bool) ([]int64, error) {
	return r.set(ctx, `UPDATE works SET closed = $2 WHERE id = $1 RETURNING id`,
		`SELECT id FROM works WHERE id = $1 OR parent_work_id = $1 ORDER BY id`, id, closed)
}

// SetEditionClosed — то же для издания: возвращает все его работы и их
// служебные.
func (r *ClosedRepository) SetEditionClosed(ctx context.Context, id int64, closed bool) ([]int64, error) {
	return r.set(ctx, `UPDATE editions SET closed = $2 WHERE id = $1 RETURNING id`,
		`SELECT w.id FROM works w
		 WHERE w.edition_id = $1
		    OR w.parent_work_id IN (SELECT id FROM works WHERE edition_id = $1)
		 ORDER BY w.id`, id, closed)
}

// SetJournalClosed — то же для журнала: возвращает работы его номеров.
func (r *ClosedRepository) SetJournalClosed(ctx context.Context, id int64, closed bool) ([]int64, error) {
	return r.set(ctx, `UPDATE journals SET closed = $2 WHERE id = $1 RETURNING id`,
		`SELECT work_id FROM journal_issues WHERE journal_id = $1 ORDER BY work_id`, id, closed)
}

// set пишет признак и собирает затронутые работы одной транзакцией. Нет цели —
// ErrClosedTargetNotFound, ничего не записано. Повтор с тем же значением
// безвреден и возвращает те же работы: вызывающему нужно сбросить по ним кэши.
func (r *ClosedRepository) set(ctx context.Context, update, affected string, id int64, closed bool) ([]int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var got int64
	if err := tx.QueryRow(ctx, update, id, closed).Scan(&got); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClosedTargetNotFound
		}
		return nil, fmt.Errorf("set closed: %w", err)
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
