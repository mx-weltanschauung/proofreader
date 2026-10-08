package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/audio"
	"proofreader/internal/models"
)

var (
	// ErrAudioNotFound — нет заявки, дорожки, записи или тома/главы, к
	// которым их привязывают.
	ErrAudioNotFound = errors.New("audio: not found")
	// ErrAudioConflict — действие не годится для текущего состояния строки.
	ErrAudioConflict = errors.New("audio: conflict")
	// ErrAudioDuplicate — запись с этим ключом объекта уже зарегистрирована
	// (повторная отправка той же заливки).
	ErrAudioDuplicate = errors.New("audio: duplicate")
)

// AudioRepository — очередь озвучки и синтезированные дорожки.
type AudioRepository struct {
	pool *pgxpool.Pool
}

func NewAudioRepository(pool *pgxpool.Pool) *AudioRepository {
	return &AudioRepository{pool: pool}
}

const openStatuses = `('в_очереди', 'синтезируется')`

const queueSelect = `
	SELECT q.id, q.work_id, w.title, q.chapter_id, coalesce(c.title, ''), q.status, q.error,
	       q.status_counts, coalesce(u.email, ''), q.requested_at, q.claimed_at, q.finished_at
	FROM audio_queue q
	JOIN works w ON w.id = q.work_id
	LEFT JOIN chapters c ON c.id = q.chapter_id
	LEFT JOIN users u ON u.id = q.requested_by`

func scanQueueItem(row pgx.Row) (models.AudioQueueItem, error) {
	var it models.AudioQueueItem
	var counts []byte
	err := row.Scan(&it.ID, &it.WorkID, &it.WorkTitle, &it.ChapterID, &it.ChapterTitle,
		&it.Status, &it.Error, &counts, &it.RequestedBy, &it.RequestedAt, &it.ClaimedAt, &it.FinishedAt)
	if err != nil {
		return it, err
	}
	it.StatusCounts = map[string]int{}
	if len(counts) > 0 {
		if err := json.Unmarshal(counts, &it.StatusCounts); err != nil {
			return it, fmt.Errorf("status_counts заявки %d: %w", it.ID, err)
		}
	}
	return it, nil
}

func (r *AudioRepository) listQueue(ctx context.Context, where string, args ...any) ([]models.AudioQueueItem, error) {
	rows, err := r.pool.Query(ctx, queueSelect+" "+where+" ORDER BY q.requested_at, q.id", args...)
	if err != nil {
		return nil, fmt.Errorf("очередь озвучки: %w", err)
	}
	defer rows.Close()
	var out []models.AudioQueueItem
	for rows.Next() {
		it, err := scanQueueItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *AudioRepository) QueueItem(ctx context.Context, id int64) (models.AudioQueueItem, error) {
	it, err := scanQueueItem(r.pool.QueryRow(ctx, queueSelect+" WHERE q.id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrAudioNotFound
	}
	return it, err
}

// Enqueue ставит главу (или том при chapterID == nil) в очередь. Если
// незавершённая заявка на то же уже есть, отдаёт её (created = false).
//
// Вставка и чтение существующей — два шага: между ними открытая заявка
// может завершиться, тогда чтение ничего не найдёт, и попытка повторяется.
func (r *AudioRepository) Enqueue(ctx context.Context, workID int64, chapterID *int64, by *int64) (models.AudioQueueItem, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var id int64
		err := r.pool.QueryRow(ctx, `
			INSERT INTO audio_queue (work_id, chapter_id, requested_by) VALUES ($1, $2, $3)
			ON CONFLICT (work_id, (coalesce(chapter_id, 0))) WHERE status IN `+openStatuses+`
			DO NOTHING RETURNING id`, workID, chapterID, by).Scan(&id)
		if err == nil {
			it, err := r.QueueItem(ctx, id)
			return it, true, err
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return models.AudioQueueItem{}, false, ErrAudioNotFound
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return models.AudioQueueItem{}, false, fmt.Errorf("постановка в очередь: %w", err)
		}
		items, err := r.listQueue(ctx,
			`WHERE q.work_id = $1 AND coalesce(q.chapter_id, 0) = coalesce($2::bigint, 0) AND q.status IN `+openStatuses,
			workID, chapterID)
		if err != nil {
			return models.AudioQueueItem{}, false, err
		}
		if len(items) == 1 {
			return items[0], false, nil
		}
	}
	return models.AudioQueueItem{}, false, fmt.Errorf("постановка в очередь: заявка меняется слишком быстро")
}

func (r *AudioRepository) ListOpenQueue(ctx context.Context) ([]models.AudioQueueItem, error) {
	return r.listQueue(ctx, `WHERE q.status IN ('в_очереди', 'синтезируется', 'ошибка')`)
}

func (r *AudioRepository) StaleSummary(ctx context.Context) ([]models.AudioStaleWork, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.work_id, w.title, count(*)::int FROM audio_tracks t
		JOIN works w ON w.id = t.work_id
		WHERE t.stale GROUP BY t.work_id, w.title ORDER BY w.title, t.work_id`)
	if err != nil {
		return nil, fmt.Errorf("сводка устаревших: %w", err)
	}
	defer rows.Close()
	var out []models.AudioStaleWork
	for rows.Next() {
		var s models.AudioStaleWork
		if err := rows.Scan(&s.WorkID, &s.WorkTitle, &s.Stale); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// conflictOrMissing различает «строки нет» и «строка в другом состоянии»
// после UPDATE/DELETE, не задевшего ни одной строки.
func (r *AudioRepository) conflictOrMissing(ctx context.Context, id int64) error {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audio_queue WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrAudioConflict
	}
	return ErrAudioNotFound
}

func (r *AudioRepository) CancelQueueItem(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM audio_queue WHERE id = $1 AND status IN ('в_очереди', 'ошибка')`, id)
	if err != nil {
		return fmt.Errorf("снятие заявки %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return r.conflictOrMissing(ctx, id)
	}
	return nil
}

func (r *AudioRepository) RetryQueueItem(ctx context.Context, id int64) (models.AudioQueueItem, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE audio_queue SET status = 'в_очереди', error = '', claimed_at = NULL, finished_at = NULL
		WHERE id = $1 AND status = 'ошибка'`, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.AudioQueueItem{}, ErrAudioConflict
		}
		return models.AudioQueueItem{}, fmt.Errorf("повтор заявки %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return models.AudioQueueItem{}, r.conflictOrMissing(ctx, id)
	}
	return r.QueueItem(ctx, id)
}

// Claim забирает заявки worker'у. ids == nil — все ждущие. Зависшая
// «синтезируется» (claimed_at старше 6 ч) забирается всегда, свежая — только
// при reclaim. SKIP LOCKED: два worker'а разом не получат одну заявку.
func (r *AudioRepository) Claim(ctx context.Context, ids []int64, reclaim bool) ([]models.AudioQueueItem, error) {
	all := ids == nil
	if all {
		ids = []int64{}
	}
	rows, err := r.pool.Query(ctx, `
		WITH picked AS (
			SELECT id FROM audio_queue
			WHERE ($1 OR id = ANY($2))
			  AND (status = 'в_очереди'
			       OR (status = 'синтезируется'
			           AND ($3 OR claimed_at < now() - interval '6 hours')))
			ORDER BY requested_at, id
			FOR UPDATE SKIP LOCKED)
		UPDATE audio_queue q SET status = 'синтезируется', claimed_at = now(), error = ''
		FROM picked WHERE q.id = picked.id
		RETURNING q.id`, all, ids, reclaim)
	if err != nil {
		return nil, fmt.Errorf("забор заявок: %w", err)
	}
	var claimed []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		claimed = append(claimed, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(claimed) == 0 {
		return nil, nil
	}
	return r.listQueue(ctx, `WHERE q.id = ANY($1)`, claimed)
}

// FinishQueueItem — итог от worker'а. Только из «синтезируется» и только с
// отметкой того забора, которым заявка взята сейчас: после --reclaim (или 6 ч)
// прежний worker, если ещё жив, иначе закрыл бы чужую заявку. Отметку Claim
// ставит заново при каждом заборе, поэтому она и есть токен — отдельной
// колонки не нужно.
func (r *AudioRepository) FinishQueueItem(ctx context.Context, id int64, claimedAt time.Time, status, errText string, counts map[string]int) error {
	if counts == nil {
		counts = map[string]int{}
	}
	raw, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE audio_queue SET status = $2, error = $3, status_counts = $4::jsonb,
		       finished_at = CASE WHEN $2 IN ('готово', 'ошибка') THEN now() END,
		       claimed_at = CASE WHEN $2 = 'в_очереди' THEN NULL ELSE claimed_at END
		WHERE id = $1 AND status = 'синтезируется' AND claimed_at = $5`, id, status, errText, string(raw), claimedAt)
	if err != nil {
		return fmt.Errorf("итог заявки %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return r.conflictOrMissing(ctx, id)
	}
	return nil
}

const trackColumns = `id, work_id, title, start_page, end_page, s3_key, duration_ms, bytes, md5,
	recipe_sha256, pages_sha256, stale, created_at`

func scanTrack(row pgx.Row) (models.AudioTrack, error) {
	var t models.AudioTrack
	err := row.Scan(&t.ID, &t.WorkID, &t.Title, &t.StartPage, &t.EndPage, &t.S3Key, &t.DurationMS,
		&t.Bytes, &t.MD5, &t.RecipeSHA256, &t.PagesSHA256, &t.Stale, &t.CreatedAt)
	return t, err
}

func (r *AudioRepository) listTracks(ctx context.Context, where string, args ...any) ([]models.AudioTrack, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+trackColumns+` FROM audio_tracks `+where+` ORDER BY start_page, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("дорожки: %w", err)
	}
	defer rows.Close()
	var out []models.AudioTrack
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *AudioRepository) ListTracks(ctx context.Context, workID int64) ([]models.AudioTrack, error) {
	return r.listTracks(ctx, `WHERE work_id = $1`, workID)
}

func (r *AudioRepository) StaleTracks(ctx context.Context, workID int64) ([]models.AudioTrack, error) {
	return r.listTracks(ctx, `WHERE work_id = $1 AND stale`, workID)
}

// Track — одна дорожка: ключ для подписи ссылки и заголовок для имени файла.
func (r *AudioRepository) Track(ctx context.Context, id int64) (models.AudioTrack, error) {
	t, err := scanTrack(r.pool.QueryRow(ctx, `SELECT `+trackColumns+` FROM audio_tracks WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrAudioNotFound
	}
	return t, err
}

func (r *AudioRepository) KeyReferenced(ctx context.Context, key string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audio_tracks WHERE s3_key = $1)
		OR EXISTS (SELECT 1 FROM audio_recordings WHERE s3_key = $1)`, key).Scan(&ok)
	return ok, err
}

// RegisterTracks регистрирует дорожки одной заявки: пересчитывает отпечаток
// текста, удаляет прежние дорожки тома, пересекающиеся с новыми, и
// вставляет новые — одной транзакцией.
//
// Регистрации одного тома идут по очереди (консультативный замок по тому):
// иначе DELETE каждой из двух одновременных не видит незакоммиченной вставки
// другой, и пересекающиеся дорожки остаются обе. Вторая дожидается первой и
// своим DELETE уносит её дорожки в freed — побеждает последняя.
//
// Порядок замков обязателен: замок тома, затем полосы (FOR SHARE), потом дорожки —
// тот же, что у SaveEdit (UPDATE pages, затем UPDATE audio_tracks). Правка,
// успевшая первой, закончится до пересчёта, и отпечаток её увидит; правка,
// пришедшая позже, дождётся коммита и пометит уже вставленные дорожки.
// Обратный порядок давал бы взаимную блокировку.
func (r *AudioRepository) RegisterTracks(ctx context.Context, workID int64, tracks []models.AudioTrack) ([]models.AudioTrack, []models.AudioTrack, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("регистрация дорожек: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit это no-op

	// Префикс — чтобы не делить ключ с пределами частоты (одно пространство
	// консультативных замков на всю базу).
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(fmt.Sprintf("audio_tracks:%d", workID)))
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(hasher.Sum64())); err != nil {
		return nil, nil, fmt.Errorf("замок регистрации тома %d: %w", workID, err)
	}

	starts := make([]int32, len(tracks))
	ends := make([]int32, len(tracks))
	newKeys := map[string]bool{}
	for i := range tracks {
		t := &tracks[i]
		rows, err := tx.Query(ctx, `
			SELECT content_markdown FROM pages
			WHERE work_id = $1 AND page_number BETWEEN $2 AND $3
			ORDER BY page_number FOR SHARE`, workID, t.StartPage, t.EndPage)
		if err != nil {
			return nil, nil, fmt.Errorf("текст полос %d—%d: %w", t.StartPage, t.EndPage, err)
		}
		var texts []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				rows.Close()
				return nil, nil, err
			}
			texts = append(texts, s)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		t.Stale = audio.PagesSHA256(texts) != t.PagesSHA256
		starts[i], ends[i] = int32(t.StartPage), int32(t.EndPage)
		newKeys[t.S3Key] = true
	}

	rows, err := tx.Query(ctx, `
		DELETE FROM audio_tracks t
		WHERE t.work_id = $1 AND EXISTS (
			SELECT 1 FROM unnest($2::int[], $3::int[]) AS n(s, e)
			WHERE t.start_page <= n.e AND t.end_page >= n.s)
		RETURNING `+trackColumns, workID, starts, ends)
	if err != nil {
		return nil, nil, fmt.Errorf("снятие прежних дорожек: %w", err)
	}
	var freed []models.AudioTrack
	for rows.Next() {
		old, err := scanTrack(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		if !newKeys[old.S3Key] {
			freed = append(freed, old)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	for i := range tracks {
		t := &tracks[i]
		t.WorkID = workID
		if err := tx.QueryRow(ctx, `
			INSERT INTO audio_tracks (work_id, title, start_page, end_page, s3_key, duration_ms, bytes, md5,
			                          recipe_sha256, pages_sha256, stale)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, created_at`,
			workID, t.Title, t.StartPage, t.EndPage, t.S3Key, t.DurationMS, t.Bytes, t.MD5,
			t.RecipeSHA256, t.PagesSHA256, t.Stale,
		).Scan(&t.ID, &t.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("вставка дорожки %d—%d: %w", t.StartPage, t.EndPage, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("регистрация дорожек: %w", err)
	}
	return tracks, freed, nil
}
