package repository

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// ErrMarginNoteLimit — у читателя уже набрался потолок заметок.
var ErrMarginNoteLimit = errors.New("margin note limit reached")

// marginNotesPerOwner — потолок заметок одного читателя; переменная, а не
// константа, чтобы тест мог снизить его до двух.
var marginNotesPerOwner = models.MarginNotesPerOwner

// MarginNoteRepository — личные заметки читателя на полях. Закрытости у
// заметки нет: при includeClosed=false её скрывает сам запрос по закрытой
// работе, главе или полосе (представления closed_works/closed_chapters/
// closed_pages), поэтому закрытое не утекает через «мои заметки».
type MarginNoteRepository struct {
	pool *pgxpool.Pool
}

func NewMarginNoteRepository(pool *pgxpool.Pool) *MarginNoteRepository {
	return &MarginNoteRepository{pool: pool}
}

// Номера полос и признак «полосы больше нет» берутся из живой таблицы pages
// (заметка держит id с ON DELETE SET NULL), снимок номера остаётся в
// snapshot. Адресный слаг работы — теми же колонками, что у /api/works
// (worksWithSlug + workSlugFrom), второго транслитератора нет. Самая узкая
// накрывающая глава — тем же правилом, что ChapterRepository.FindByPage.
const marginNoteSelect = `
	SELECT mn.id, mn.owner_id, mn.work_id, mn.target, mn.chapter_id, mn.chapter_title,
	       sp.page_number, ep.page_number,
	       (mn.target = 'quote' AND (sp.id IS NULL OR ep.id IS NULL)),
	       mn.start_page_number, mn.quote_key, mn.quote_tail, mn.quote_text, mn.body,
	       mn.created_at, mn.updated_at,
	       works.title, works.role, works.precedes_volume,
	       works.slug_edition, works.slug_volume, works.slug_part,
	       works.page_offset, works.numbering_style,
	       COALESCE(cc.title, '')
	FROM margin_notes mn
	JOIN ` + worksWithSlug + ` ON works.id = mn.work_id
	LEFT JOIN pages sp ON sp.id = mn.start_page_id
	LEFT JOIN pages ep ON ep.id = mn.end_page_id
	LEFT JOIN LATERAL (
		SELECT c.title FROM chapters c
		WHERE c.work_id = mn.work_id AND sp.page_number BETWEEN c.start_page AND c.end_page
		ORDER BY c.end_page - c.start_page, c.id
		LIMIT 1
	) cc ON true`

// marginNoteOpen отсекает закрытое: работу, главу и обе полосы заметки.
// Только NOT EXISTS с равенством по id, не NOT IN (SELECT id FROM …): NOT IN
// строит представление целиком — closed_pages тогда проходит всю таблицу
// pages на каждый запрос списка (замер на 200 тыс. полос — 2 с против 2 мс),
// а NOT EXISTS проталкивает id в представление и берёт полосу по ключу.
// Пустой id (главу или полосу снесли) даёт NOT EXISTS истину — заметка
// проходит, как и должна.
//
// Полоса снесена (id обнулён), а под тем же номером в том же томе лежит
// новая (перезаливка) — тогда судит она: цитата, взятая с полосы, которая
// теперь закрыта, наружу не идёт. Без этого условия заметка с потерянной
// полосой проходила бы проверку полос вовсе не проверенной.
const marginNoteOpen = `
	AND NOT EXISTS (SELECT 1 FROM closed_works c WHERE c.id = mn.work_id)
	AND NOT EXISTS (SELECT 1 FROM closed_chapters c WHERE c.id = mn.chapter_id)
	AND NOT EXISTS (SELECT 1 FROM closed_pages c WHERE c.id = mn.start_page_id)
	AND NOT EXISTS (SELECT 1 FROM closed_pages c WHERE c.id = mn.end_page_id)
	AND (mn.start_page_id IS NOT NULL OR NOT EXISTS (SELECT 1 FROM closed_pages c
		WHERE c.work_id = mn.work_id AND c.page_number = mn.start_page_number))
	AND (mn.end_page_id IS NOT NULL OR NOT EXISTS (SELECT 1 FROM closed_pages c
		WHERE c.work_id = mn.work_id AND c.page_number = mn.end_page_number))`

func scanMarginNote(row rowScanner) (*models.MarginNote, error) {
	var n models.MarginNote
	var role string
	var precedes *int
	var slugEdition string
	var slugVolume *int
	var slugPart *string
	if err := row.Scan(
		&n.ID, &n.OwnerID, &n.WorkID, &n.Target, &n.ChapterID, &n.ChapterTitle,
		&n.StartPage, &n.EndPage, &n.PageMissing,
		&n.SnapshotStart, &n.QuoteKey, &n.QuoteTail, &n.QuoteText, &n.Body,
		&n.CreatedAt, &n.UpdatedAt,
		&n.WorkTitle, &role, &precedes, &slugEdition, &slugVolume, &slugPart,
		&n.PageOffset, &n.NumberingStyle,
		&n.ContextChapter,
	); err != nil {
		return nil, err
	}
	n.WorkSlug = workSlugFrom(role, slugEdition, slugVolume, slugPart, precedes, n.WorkTitle)
	return &n, nil
}

func marginOpenClause(includeClosed bool) string {
	if includeClosed {
		return ""
	}
	return marginNoteOpen
}

func (r *MarginNoteRepository) get(ctx context.Context, id int64) (*models.MarginNote, error) {
	n, err := scanMarginNote(r.pool.QueryRow(ctx, marginNoteSelect+` WHERE mn.id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("margin note not found")
		}
		return nil, fmt.Errorf("failed to get margin note: %w", err)
	}
	return n, nil
}

// Create пишет заметку; счёт и вставка идут одной транзакцией под замком
// читателя — иначе залп параллельных запросов проходил бы мимо потолка.
func (r *MarginNoteRepository) Create(ctx context.Context, in models.MarginNoteInput) (*models.MarginNote, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // откат после успешного Commit безвреден

	// Ключ блокировки считаем в Go (hashtext() — недокументированная
	// функция); префикс таблицы разводит эту очередь с прочими рельсами.
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("margin_notes:" + strconv.FormatInt(in.OwnerID, 10)))
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(hasher.Sum64())); err != nil {
		return nil, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM margin_notes WHERE owner_id = $1`, in.OwnerID).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to count margin notes: %w", err)
	}
	if count >= marginNotesPerOwner {
		return nil, ErrMarginNoteLimit
	}

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO margin_notes
			(owner_id, work_id, target, chapter_id, chapter_title,
			 start_page_id, end_page_id, start_page_number, end_page_number,
			 quote_key, quote_tail, quote_text, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id`,
		in.OwnerID, in.WorkID, in.Target, in.ChapterID, in.ChapterTitle,
		in.StartPageID, in.EndPageID, in.StartPageNumber, in.EndPageNumber,
		in.QuoteKey, in.QuoteTail, in.QuoteText, in.Body,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create margin note: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return r.get(ctx, id)
}

// UpdateBody меняет текст заметки. Чужая, закрытая (без includeClosed) и
// несуществующая заметка — один и тот же отказ.
func (r *MarginNoteRepository) UpdateBody(ctx context.Context, ownerID, id int64, body string, includeClosed bool) (*models.MarginNote, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE margin_notes mn SET body = $3, updated_at = now()
		WHERE mn.id = $2 AND mn.owner_id = $1`+marginOpenClause(includeClosed),
		ownerID, id, body)
	if err != nil {
		return nil, fmt.Errorf("failed to update margin note: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("margin note not found")
	}
	return r.get(ctx, id)
}

// Delete удаляет заметку владельца; отказы те же, что у UpdateBody.
func (r *MarginNoteRepository) Delete(ctx context.Context, ownerID, id int64, includeClosed bool) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM margin_notes mn
		WHERE mn.id = $2 AND mn.owner_id = $1`+marginOpenClause(includeClosed),
		ownerID, id)
	if err != nil {
		return fmt.Errorf("failed to delete margin note: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("margin note not found")
	}
	return nil
}

func (r *MarginNoteRepository) collect(ctx context.Context, query string, args ...any) ([]*models.MarginNote, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list margin notes: %w", err)
	}
	defer rows.Close()
	out := []*models.MarginNote{}
	for rows.Next() {
		n, err := scanMarginNote(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan margin note: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListByWork — заметки читателя к одному тому: сперва без полосы (том,
// глава), затем по номеру полосы.
func (r *MarginNoteRepository) ListByWork(ctx context.Context, ownerID, workID int64, includeClosed bool) ([]*models.MarginNote, error) {
	return r.collect(ctx, marginNoteSelect+`
		WHERE mn.owner_id = $1 AND mn.work_id = $2`+marginOpenClause(includeClosed)+`
		ORDER BY mn.start_page_number NULLS FIRST, mn.created_at, mn.id`,
		ownerID, workID)
}

// ListByOwner — «Мои заметки»: свежие первыми, при workID — одного тома.
// total считается тем же условием, что и выборка.
func (r *MarginNoteRepository) ListByOwner(ctx context.Context, ownerID int64, workID *int64, limit, offset int, includeClosed bool) ([]*models.MarginNote, int, error) {
	where := ` WHERE mn.owner_id = $1 AND ($2::bigint IS NULL OR mn.work_id = $2)` + marginOpenClause(includeClosed)

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM margin_notes mn`+where, ownerID, workID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count margin notes: %w", err)
	}
	notes, err := r.collect(ctx, marginNoteSelect+where+`
		ORDER BY mn.created_at DESC, mn.id DESC LIMIT $3 OFFSET $4`,
		ownerID, workID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return notes, total, nil
}
