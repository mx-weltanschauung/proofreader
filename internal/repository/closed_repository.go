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

const scansHiddenByCase = `work_scans_hidden_by(works.id)`

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

// ScansHiddenBy — источник ограничения сканов работы; пусто — сканы открыты.
func (r *ClosedRepository) ScansHiddenBy(ctx context.Context, workID int64) (string, error) {
	var by *string
	if err := r.pool.QueryRow(ctx, `SELECT work_scans_hidden_by($1)`, workID).Scan(&by); err != nil {
		return "", fmt.Errorf("work_scans_hidden_by: %w", err)
	}
	if by == nil {
		return "", nil
	}
	return *by, nil
}

// WorksWithHiddenScans — работы с ограничением на любом уровне, включая
// диапазон главы: оригинал такой работы раскрывает закрытые изображения.
func (r *ClosedRepository) WorksWithHiddenScans(ctx context.Context) (map[int64]bool, error) {
	return r.idSet(ctx, `SELECT id FROM scans_hidden_works
		UNION SELECT work_id FROM scans_hidden_chapters`)
}

// ScansHiddenSpans — диапазоны глав, чьи сканы скрыты по своему признаку
// либо по признаку предка-главы.
func (r *ClosedRepository) ScansHiddenSpans(ctx context.Context, workID int64) ([]models.PageSpan, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT start_page, end_page FROM scans_hidden_chapters WHERE work_id=$1 ORDER BY start_page, end_page`, workID)
	if err != nil {
		return nil, fmt.Errorf("scans hidden spans: %w", err)
	}
	defer rows.Close()
	spans := []models.PageSpan{}
	for rows.Next() {
		var span models.PageSpan
		if err := rows.Scan(&span.Start, &span.End); err != nil {
			return nil, err
		}
		spans = append(spans, span)
	}
	return spans, rows.Err()
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

// AllClosedChapterIDs — все главы, закрытые действующе, по всем работам: для
// событий, чьи цели разбросаны по томам.
func (r *ClosedRepository) AllClosedChapterIDs(ctx context.Context) (map[int64]bool, error) {
	return r.idSet(ctx, `SELECT id FROM closed_chapters`)
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

func (r *ClosedRepository) SetWorkScansHidden(ctx context.Context, id int64, hidden bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.setScope(ctx, ClosureWork, "scans", `UPDATE works SET scans_hidden = $2 WHERE id = $1 RETURNING title`,
		`WITH RECURSIVE affected AS (
			SELECT id FROM works WHERE id = $1
			UNION SELECT w.id FROM works w JOIN affected a ON w.parent_work_id = a.id
		) SELECT id FROM affected ORDER BY id`, id, hidden, actor, reason)
}

func (r *ClosedRepository) SetEditionScansHidden(ctx context.Context, id int64, hidden bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.setScope(ctx, ClosureEdition, "scans", `UPDATE editions SET scans_hidden = $2 WHERE id = $1 RETURNING title`,
		`WITH RECURSIVE affected AS (
			SELECT id FROM works WHERE edition_id = $1
			UNION SELECT w.id FROM works w JOIN affected a ON w.parent_work_id = a.id
		) SELECT id FROM affected ORDER BY id`, id, hidden, actor, reason)
}

func (r *ClosedRepository) SetJournalScansHidden(ctx context.Context, id int64, hidden bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.setScope(ctx, ClosureJournal, "scans", `UPDATE journals SET scans_hidden = $2 WHERE id = $1 RETURNING title`,
		`WITH RECURSIVE affected AS (
			SELECT work_id AS id FROM journal_issues WHERE journal_id = $1
			UNION SELECT w.id FROM works w JOIN affected a ON w.parent_work_id = a.id
		) SELECT id FROM affected ORDER BY id`, id, hidden, actor, reason)
}

func (r *ClosedRepository) SetChapterScansHidden(ctx context.Context, id int64, hidden bool, actor models.ClosureActor, reason string) ([]int64, error) {
	return r.setScope(ctx, ClosureChapter, "scans", `UPDATE chapters SET scans_hidden = $2 WHERE id = $1 RETURNING title`,
		`SELECT work_id FROM chapters WHERE id = $1`, id, hidden, actor, reason)
}

// set пишет признак, строку журнала и собирает затронутые работы одной
// транзакцией. Нет цели — ErrClosedTargetNotFound, ничего не записано. Повтор
// с тем же значением безвреден, тоже пишет строку и возвращает те же работы:
// вызывающему нужно сбросить по ним кэши.
func (r *ClosedRepository) set(ctx context.Context, kind, update, affected string, id int64, closed bool,
	actor models.ClosureActor, reason string) ([]int64, error) {
	return r.setScope(ctx, kind, "full", update, affected, id, closed, actor, reason)
}

func (r *ClosedRepository) setScope(ctx context.Context, kind, scope, update, affected string, id int64, closed bool,
	actor models.ClosureActor, reason string) ([]int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if scope == "scans" {
		if err := lockScanStructure(ctx, tx); err != nil {
			return nil, err
		}
	}
	var title string
	if err := tx.QueryRow(ctx, update, id, closed).Scan(&title); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClosedTargetNotFound
		}
		return nil, fmt.Errorf("set closed: %w", err)
	}
	if err := insertClosureScopeTx(ctx, tx, kind, scope, id, title, closed, actor, reason); err != nil {
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
	return insertClosureScopeTx(ctx, tx, kind, "full", id, title, closed, actor, reason)
}

func insertClosureScopeTx(ctx context.Context, tx pgx.Tx, kind, scope string, id int64, title string, closed bool,
	actor models.ClosureActor, reason string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO closures
		(entity_kind, scope, entity_id, title_snapshot, closed, reason, actor_id, actor_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		kind, scope, id, title, closed, reason, actor.ID, actor.Name); err != nil {
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

func (r *ClosedRepository) ScansHiddenChapterIDs(ctx context.Context, workID int64) (map[int64]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM scans_hidden_chapters WHERE work_id = $1`, workID)
	if err != nil {
		return nil, err
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

// ClosuresGeneration — поколение закрытий: наибольший номер строки журнала
// (0 — журнал пуст). Сдвигается любым закрытием и открытием; краулерская
// половина подмешивает его в ETag (seo.Source.Closures).
func (r *ClosedRepository) ClosuresGeneration(ctx context.Context) (int64, error) {
	var gen int64
	if err := r.pool.QueryRow(ctx, `SELECT coalesce(max(id), 0) FROM closures`).Scan(&gen); err != nil {
		return 0, fmt.Errorf("closures generation: %w", err)
	}
	return gen, nil
}

const closureColumns = `entity_kind, scope, entity_id, title_snapshot, closed, reason, actor_name, created_at`

func scanClosure(row rowScanner) (*models.Closure, error) {
	var c models.Closure
	if err := row.Scan(&c.Kind, &c.Scope, &c.EntityID, &c.Title, &c.Closed, &c.Reason, &c.ActorName, &c.At); err != nil {
		return nil, err
	}
	return &c, nil
}

// LatestClosure — последняя строка журнала по сущности; nil, nil — строк нет.
func (r *ClosedRepository) LatestClosure(ctx context.Context, kind string, id int64, scope string) (*models.Closure, error) {
	c, err := scanClosure(r.pool.QueryRow(ctx, `SELECT `+closureColumns+` FROM closures
		WHERE entity_kind = $1 AND entity_id = $2 AND scope = $3 ORDER BY created_at DESC, id DESC LIMIT 1`, kind, id, scope))
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

// ListActiveClosures читает собственные признаки из объектов, не восстанавливая
// состояние по журналу. Последнее закрытие служит лишь подписью строки.
func (r *ClosedRepository) ListActiveClosures(ctx context.Context, limit, offset int) ([]models.ActiveClosure, error) {
	const query = `WITH work_flags AS (
		SELECT w.id, w.title, w.closed, w.scans_hidden,
			(coalesce(p.closed, false) OR coalesce(pe.closed, false)
			 OR coalesce(e.closed, false) OR coalesce(j.closed, false)) AS inherited_full,
			(coalesce(work_scans_hidden_by(w.parent_work_id), '') <> ''
			 OR coalesce(e.scans_hidden, false) OR coalesce(j.scans_hidden, false)) AS inherited_scans
		FROM works w
		LEFT JOIN works p ON p.id = w.parent_work_id
		LEFT JOIN editions pe ON pe.id = p.edition_id
		LEFT JOIN editions e ON e.id = w.edition_id
		LEFT JOIN journal_issues ji ON ji.work_id = w.id
		LEFT JOIN journals j ON j.id = ji.journal_id
		WHERE w.closed OR w.scans_hidden
	), chapter_flags AS (
		SELECT c.id, c.work_id, c.title, c.closed, c.scans_hidden,
			(coalesce(a.closed, false) OR coalesce(work_closed_by(c.work_id), '') <> '') AS inherited_full,
			(coalesce(a.scans_hidden, false) OR coalesce(work_scans_hidden_by(c.work_id), '') <> '') AS inherited_scans
		FROM chapters c
		LEFT JOIN LATERAL (
			WITH RECURSIVE ancestors AS (
				SELECT p.id, p.parent_id, p.closed, p.scans_hidden
				FROM chapters p WHERE p.id = c.parent_id AND p.id <> c.id
				UNION
				SELECT p.id, p.parent_id, p.closed, p.scans_hidden
				FROM chapters p JOIN ancestors a ON p.id = a.parent_id
				WHERE p.id <> c.id
			)
			SELECT bool_or(closed) AS closed, bool_or(scans_hidden) AS scans_hidden FROM ancestors
		) a ON true
		WHERE c.closed OR c.scans_hidden
	), flags AS (
		SELECT 'work' AS kind, 'full' AS scope, id AS entity_id, title,
			NULL::bigint AS work_id, NULL::text AS slug, inherited_full AS inherited
		FROM work_flags WHERE closed
		UNION ALL
		SELECT 'work', 'scans', id, title, NULL::bigint, NULL::text, inherited_scans
		FROM work_flags WHERE scans_hidden
		UNION ALL
		SELECT 'edition', 'full', id, title, NULL::bigint, NULL::text, false
		FROM editions WHERE closed
		UNION ALL
		SELECT 'edition', 'scans', id, title, NULL::bigint, NULL::text, false
		FROM editions WHERE scans_hidden
		UNION ALL
		SELECT 'journal', 'full', id, title, NULL::bigint, slug, false
		FROM journals WHERE closed
		UNION ALL
		SELECT 'journal', 'scans', id, title, NULL::bigint, slug, false
		FROM journals WHERE scans_hidden
		UNION ALL
		SELECT 'chapter', 'full', id, title, work_id, NULL::text, inherited_full
		FROM chapter_flags WHERE closed
		UNION ALL
		SELECT 'chapter', 'scans', id, title, work_id, NULL::text, inherited_scans
		FROM chapter_flags WHERE scans_hidden
	)
	SELECT f.kind, f.scope, f.entity_id, f.title, f.work_id, f.slug,
		a.created_at, coalesce(a.reason, ''), coalesce(a.actor_name, ''), f.inherited
	FROM flags f
	LEFT JOIN LATERAL (
		SELECT created_at, reason, actor_name FROM closures
		WHERE entity_kind = f.kind AND entity_id = f.entity_id
		  AND scope = f.scope AND closed
		ORDER BY created_at DESC, id DESC LIMIT 1
	) a ON true
	ORDER BY a.created_at DESC NULLS LAST, f.kind, f.entity_id, f.scope
	LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list active closures: %w", err)
	}
	defer rows.Close()
	out := []models.ActiveClosure{}
	for rows.Next() {
		var c models.ActiveClosure
		if err := rows.Scan(&c.Kind, &c.Scope, &c.EntityID, &c.Title, &c.WorkID,
			&c.Slug, &c.At, &c.Reason, &c.ActorName, &c.Inherited); err != nil {
			return nil, fmt.Errorf("scan active closure: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
