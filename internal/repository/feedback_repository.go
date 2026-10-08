package repository

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// FeedbackRepository handles feedback data access.
type FeedbackRepository struct {
	pool *pgxpool.Pool
}

// NewFeedbackRepository creates a new feedback repository.
func NewFeedbackRepository(pool *pgxpool.Pool) *FeedbackRepository {
	return &FeedbackRepository{pool: pool}
}

// Create writes a new letter, without checking the rate limit.
//
// В обработчике не используется — это тот самый неатомарный путь (сначала
// счёт, потом запись), который убрали из боевого кода из-за гонки под
// параллельной нагрузкой. Годится только для засева фикстур в тестах.
// Боевой путь — CreateWithinLimit: он держит предел частоты под одной
// блокировкой вместе с записью.
func (r *FeedbackRepository) Create(ctx context.Context, f *models.Feedback) error {
	query := `
		INSERT INTO feedback (message, source_path, ip_hash)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`

	err := r.pool.QueryRow(ctx, query, f.Message, f.SourcePath, f.IPHash).
		Scan(&f.ID, &f.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create feedback: %w", err)
	}

	return nil
}

// List returns letters newest first. handled == nil — все; иначе только
// разобранные или только новые.
func (r *FeedbackRepository) List(ctx context.Context, handled *bool, limit int) ([]*models.Feedback, error) {
	// Предикат подставляется из трёх заранее известных строк, а не собирается
	// из значения: конкатенация пользовательского ввода в SQL — та самая
	// дверь, которую здесь открывать незачем.
	where := ""
	if handled != nil {
		if *handled {
			where = "WHERE handled_at IS NOT NULL"
		} else {
			where = "WHERE handled_at IS NULL"
		}
	}

	query := fmt.Sprintf(`
		SELECT id, message, source_path, ip_hash, handled_at, created_at
		FROM feedback
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, where)

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list feedback: %w", err)
	}
	defer rows.Close()

	out := make([]*models.Feedback, 0)
	for rows.Next() {
		var f models.Feedback
		if err := rows.Scan(&f.ID, &f.Message, &f.SourcePath,
			&f.IPHash, &f.HandledAt, &f.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan feedback: %w", err)
		}
		out = append(out, &f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read feedback rows: %w", err)
	}

	return out, nil
}

// CountUnread returns how many letters are still unhandled.
func (r *FeedbackRepository) CountUnread(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM feedback WHERE handled_at IS NULL`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count unread feedback: %w", err)
	}

	return count, nil
}

// CreateWithinLimit пишет письмо, если предел частоты по отметке адреса ещё не
// исчерпан, и сообщает, приняли ли.
//
// Счёт и вставка идут в ОДНОЙ транзакции под консультативной блокировкой по
// отметке. Без неё это «проверил — записал» двумя шагами, и бот, шлющий
// параллельно, проскакивает мимо предела целиком: двадцать одновременных писем
// при пределе пять проходят все двадцать.
func (r *FeedbackRepository) CreateWithinLimit(ctx context.Context, f *models.Feedback, limit int, since time.Time) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // откат после успешного Commit безвреден

	// Ключ блокировки считаем в Go, не в SQL: hashtext() в Postgres —
	// внутренняя недокументированная функция, а IPHash к тому же не всегда
	// hex (безадресные письма используют текстовое ведро).
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(f.IPHash))
	lockKey := int64(hasher.Sum64())

	// Транзакционная блокировка: снимается сама на Commit/Rollback, руками
	// отпускать не надо.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM feedback WHERE ip_hash = $1 AND created_at > $2`,
		f.IPHash, since).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to count recent feedback: %w", err)
	}
	if count >= limit {
		return false, nil
	}

	query := `
		-- contact не пишется: читальня не собирает персональных данных.
		-- Колонка пока остаётся в схеме (DEFAULT ''), дроп — отдельной
		-- миграцией, когда общая база разработки освободится.
		INSERT INTO feedback (message, source_path, ip_hash)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`
	if err := tx.QueryRow(ctx, query, f.Message, f.SourcePath, f.IPHash).
		Scan(&f.ID, &f.CreatedAt); err != nil {
		return false, fmt.Errorf("failed to create feedback: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return true, nil
}

// SetHandled marks a letter handled (or puts it back among the new ones).
func (r *FeedbackRepository) SetHandled(ctx context.Context, id int64, handled bool) error {
	var query string
	if handled {
		query = `UPDATE feedback SET handled_at = now() WHERE id = $1`
	} else {
		query = `UPDATE feedback SET handled_at = NULL WHERE id = $1`
	}

	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to update feedback: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("feedback not found")
	}

	return nil
}

// Delete removes a letter for good.
func (r *FeedbackRepository) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM feedback WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete feedback: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("feedback not found")
	}

	return nil
}
