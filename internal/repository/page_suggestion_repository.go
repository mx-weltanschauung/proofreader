package repository

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// ErrSuggestionResolved возвращается, когда предложение уже разобрано:
// решение принимается один раз.
var ErrSuggestionResolved = errors.New("suggestion already resolved")

// ErrSuggestionNotDeletable возвращается, когда удалять нечего: предложения с
// таким номером нет либо оно не отклонено. Два случая намеренно неразличимы —
// разделить их стоило бы отдельного SELECT, а редактору в обоих случаях
// говорится одно и то же.
var ErrSuggestionNotDeletable = errors.New("suggestion is not rejected")

// PageSuggestionRepository handles reader suggestion data access
type PageSuggestionRepository struct {
	pool *pgxpool.Pool
}

// NewPageSuggestionRepository creates a new page suggestion repository
func NewPageSuggestionRepository(pool *pgxpool.Pool) *PageSuggestionRepository {
	return &PageSuggestionRepository{pool: pool}
}

// SuggestionRow — строка списка: предложение без текстов, плюс координаты
// полосы и признак устаревания.
//
// Текстов здесь нет намеренно: список нужен, чтобы выбрать, что разбирать, а
// пятьдесят полос по паре килобайт превратили бы его в мегабайт ответа.
type SuggestionRow struct {
	ID             int64                       `json:"id"`
	PageID         int64                       `json:"page_id"`
	WorkID         int64                       `json:"work_id"`
	WorkTitle      string                      `json:"work_title"`
	PageNumber     int                         `json:"page_number"`
	PageOffset     int                         `json:"page_offset"`
	Note           string                      `json:"note"`
	Status         models.PageSuggestionStatus `json:"status"`
	RejectReason   *models.RejectReason        `json:"reject_reason,omitempty"`
	Stale          bool                        `json:"stale"`
	LengthDelta    int                         `json:"length_delta"`
	CreatedAt      time.Time                   `json:"created_at"`
	ResolvedAt     *time.Time                  `json:"resolved_at,omitempty"`
	AuthorNickname *string                     `json:"author_nickname,omitempty"`
}

// SuggestionDetail — то же плюс три текста: основа, предложенное и текущий
// текст полосы. Экрану модерации нужны все три.
type SuggestionDetail struct {
	SuggestionRow
	BaseMarkdown     string `json:"base_markdown"`
	ProposedMarkdown string `json:"proposed_markdown"`
	CurrentMarkdown  string `json:"current_markdown"`
}

// Общая часть выборки списка: координаты полосы приезжают соединением, признак
// устаревания и объём правки считает Postgres — тексты ради них не едут.
//
// suggestionRowColumns (список колонок) и suggestionRowFrom (источник) — две
// разные константы, а не одна строка с FROM внутри: GetDetail вклеивает свои
// три текста в SELECT между колонками и FROM. Склей их заранее — и колонки,
// добавленные после JOIN, Postgres прочтёт как ещё один элемент списка FROM
// через запятую, а не как колонки SELECT.
const suggestionRowColumns = `
	s.id, s.page_id, p.work_id, w.title, p.page_number, w.page_offset,
	s.note, s.status, s.reject_reason,
	(s.status = 'новое' AND s.base_markdown IS DISTINCT FROM p.content_markdown) AS stale,
	char_length(s.proposed_markdown) - char_length(s.base_markdown) AS length_delta,
	s.created_at, s.resolved_at, u.nickname AS author_nickname`

const suggestionRowFrom = `
	FROM page_suggestions s
	JOIN pages p ON p.id = s.page_id
	JOIN works w ON w.id = p.work_id
	LEFT JOIN users u ON u.id = s.user_id`

// Create stores a new suggestion and fills in id, status and created_at.
//
// В обработчике не используется — это неатомарный путь без проверки предела.
// Годится только для засева фикстур в тестах. Боевой путь —
// CreateWithinLimit: он держит предел частоты под одной блокировкой вместе с
// записью.
func (r *PageSuggestionRepository) Create(ctx context.Context, s *models.PageSuggestion) error {
	query := `
		INSERT INTO page_suggestions
			(page_id, base_markdown, proposed_markdown, note, user_id, ip_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, status, created_at
	`
	err := r.pool.QueryRow(ctx, query,
		s.PageID, s.BaseMarkdown, s.ProposedMarkdown, s.Note, s.UserID, s.IPHash,
	).Scan(&s.ID, &s.Status, &s.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create page suggestion: %w", err)
	}
	return nil
}

// CreateWithinLimit пишет предложение, если предел частоты по отметке адреса
// ещё не исчерпан, и сообщает, приняли ли.
//
// Счёт и вставка идут в ОДНОЙ транзакции под консультативной блокировкой по
// отметке. Без неё это «проверил — записал» двумя обращениями к пулу, и залп
// параллельных подач проскакивает мимо предела целиком: замер на
// одноразовой базе — из двадцати одновременных подач при пределе пять
// прошли восемнадцать (TestPageSuggestionCreateWithinLimitRace). Для полосы
// это дороже, чем для письма: каждая строка несёт до 200 000 знаков текста.
//
// Возвращает то же, что Create: id, status и created_at заполняются в s.
func (r *PageSuggestionRepository) CreateWithinLimit(
	ctx context.Context, s *models.PageSuggestion, limit int, since time.Time,
) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // откат после успешного Commit безвреден

	// Ключ блокировки считаем в Go, не в SQL: hashtext() в Postgres —
	// внутренняя недокументированная функция.
	//
	// В хэш идёт префикс имени таблицы, а не одна отметка адреса:
	// консультативные блокировки — общее на всю базу пространство ключей, и
	// без префикса подача правки и письмо в обратную связь с одного адреса
	// (тот же ip_hash, тот же fnv) вставали бы в очередь друг за другом без
	// всякой на то причины.
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("page_suggestions:" + s.IPHash))
	lockKey := int64(hasher.Sum64())

	// Транзакционная блокировка: снимается сама на Commit/Rollback, руками
	// отпускать не надо.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM page_suggestions WHERE ip_hash = $1 AND created_at > $2`,
		s.IPHash, since).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to count recent suggestions: %w", err)
	}
	if count >= limit {
		return false, nil
	}

	query := `
		INSERT INTO page_suggestions
			(page_id, base_markdown, proposed_markdown, note, user_id, ip_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, status, created_at
	`
	if err := tx.QueryRow(ctx, query,
		s.PageID, s.BaseMarkdown, s.ProposedMarkdown, s.Note, s.UserID, s.IPHash,
	).Scan(&s.ID, &s.Status, &s.CreatedAt); err != nil {
		return false, fmt.Errorf("failed to create page suggestion: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return true, nil
}

// List returns the moderation queue, newest first, plus the total for that filter.
func (r *PageSuggestionRepository) List(
	ctx context.Context, status *models.PageSuggestionStatus, limit, offset int,
) ([]SuggestionRow, int, error) {
	where := ``
	args := []any{}
	if status != nil {
		where = ` WHERE s.status = $1`
		args = append(args, *status)
	}

	// Счётчик и выборка обязаны фильтроваться одинаково, иначе total соврёт:
	// одно и то же условие where подставляется в оба запроса.
	var total int
	countQuery := `SELECT count(*) FROM page_suggestions s` + where
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count page suggestions: %w", err)
	}

	query := `SELECT ` + suggestionRowColumns + suggestionRowFrom + where +
		fmt.Sprintf(` ORDER BY s.created_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list page suggestions: %w", err)
	}
	defer rows.Close()

	out := make([]SuggestionRow, 0)
	for rows.Next() {
		var s SuggestionRow
		if err := rows.Scan(
			&s.ID, &s.PageID, &s.WorkID, &s.WorkTitle, &s.PageNumber, &s.PageOffset,
			&s.Note, &s.Status, &s.RejectReason, &s.Stale, &s.LengthDelta,
			&s.CreatedAt, &s.ResolvedAt, &s.AuthorNickname,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan page suggestion: %w", err)
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// ListByUserID returns one reader's own suggestions, newest first.
func (r *PageSuggestionRepository) ListByUserID(
	ctx context.Context, userID int64,
) ([]SuggestionRow, error) {
	query := `SELECT ` + suggestionRowColumns + suggestionRowFrom +
		` WHERE s.user_id = $1 ORDER BY s.created_at DESC LIMIT 200`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list reader suggestions: %w", err)
	}
	defer rows.Close()

	out := make([]SuggestionRow, 0)
	for rows.Next() {
		var s SuggestionRow
		if err := rows.Scan(
			&s.ID, &s.PageID, &s.WorkID, &s.WorkTitle, &s.PageNumber, &s.PageOffset,
			&s.Note, &s.Status, &s.RejectReason, &s.Stale, &s.LengthDelta,
			&s.CreatedAt, &s.ResolvedAt, &s.AuthorNickname,
		); err != nil {
			return nil, fmt.Errorf("failed to scan reader suggestion: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetDetail returns one suggestion with all three texts.
func (r *PageSuggestionRepository) GetDetail(ctx context.Context, id int64) (*SuggestionDetail, error) {
	query := `SELECT ` + suggestionRowColumns +
		`, s.base_markdown, s.proposed_markdown, p.content_markdown` +
		suggestionRowFrom + ` WHERE s.id = $1`
	// Тексты дописаны в конец списка колонок, до FROM, поэтому порядок Scan
	// ниже повторяет порядок в suggestionRowColumns и лишь потом берёт три
	// текста.

	var d SuggestionDetail
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&d.ID, &d.PageID, &d.WorkID, &d.WorkTitle, &d.PageNumber, &d.PageOffset,
		&d.Note, &d.Status, &d.RejectReason, &d.Stale, &d.LengthDelta,
		&d.CreatedAt, &d.ResolvedAt, &d.AuthorNickname,
		&d.BaseMarkdown, &d.ProposedMarkdown, &d.CurrentMarkdown,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get page suggestion %d: %w", id, err)
	}
	return &d, nil
}

// Resolve marks a suggestion accepted or rejected. Решение принимается один
// раз: условие по статусу в WHERE, ноль затронутых строк — ErrSuggestionResolved.
func (r *PageSuggestionRepository) Resolve(
	ctx context.Context,
	id int64,
	status models.PageSuggestionStatus,
	reason *models.RejectReason,
	moderatorID int64,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE page_suggestions
		   SET status = $1, reject_reason = $2, moderator_id = $3, resolved_at = now()
		 WHERE id = $4 AND status = 'новое'
	`, status, reason, moderatorID, id)
	if err != nil {
		return fmt.Errorf("failed to resolve suggestion %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSuggestionResolved
	}
	return nil
}

// Delete сносит отклонённое предложение навсегда.
//
// Условие по статусу стоит в WHERE, а не в отдельной проверке перед удалением:
// одним заявлением гонка «редактор разбирает, пока другой удаляет» невозможна
// по построению — так же устроен Resolve выше.
func (r *PageSuggestionRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM page_suggestions WHERE id = $1 AND status = 'отклонено'`, id)
	if err != nil {
		return fmt.Errorf("failed to delete suggestion %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSuggestionNotDeletable
	}
	return nil
}

// DeleteRejected сносит все отклонённые предложения и возвращает их число.
func (r *PageSuggestionRepository) DeleteRejected(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM page_suggestions WHERE status = 'отклонено'`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete rejected suggestions: %w", err)
	}
	return tag.RowsAffected(), nil
}
