package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// AudioRecordingRepository — записи человека, прикреплённые к главам.
// Отдельная таблица, а не строки audio_tracks: регистрация синтеза их не
// удаляет, правка полосы не метит, worker о них не знает — устройством, а
// не условиями в запросах (спека, «Ручные записи»).
type AudioRecordingRepository struct {
	pool *pgxpool.Pool
}

func NewAudioRecordingRepository(pool *pgxpool.Pool) *AudioRecordingRepository {
	return &AudioRecordingRepository{pool: pool}
}

const recordingSelect = `
	SELECT r.id, r.work_id, r.chapter_id, c.title, c.start_page, c.end_page, r.position, r.reader,
	       r.s3_key, r.content_type, r.bytes, r.duration_ms, r.uploaded_by, r.created_at
	FROM audio_recordings r JOIN chapters c ON c.id = r.chapter_id`

func scanRecording(row pgx.Row) (models.AudioRecording, error) {
	var x models.AudioRecording
	err := row.Scan(&x.ID, &x.WorkID, &x.ChapterID, &x.ChapterTitle, &x.ChapterStartPage, &x.ChapterEndPage,
		&x.Position, &x.Reader, &x.S3Key, &x.ContentType, &x.Bytes, &x.DurationMS, &x.UploadedBy, &x.CreatedAt)
	return x, err
}

func (r *AudioRecordingRepository) get(ctx context.Context, q pgx.Tx, id int64) (models.AudioRecording, error) {
	x, err := scanRecording(q.QueryRow(ctx, recordingSelect+` WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return x, ErrAudioNotFound
	}
	return x, err
}

// CreateRecording ставит запись в конец главы. Строка главы запирается на
// время вставки: две одновременные заливки иначе посчитали бы одну и ту же
// «следующую» позицию.
func (r *AudioRecordingRepository) CreateRecording(ctx context.Context, rec *models.AudioRecording) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var owner int64
	err = tx.QueryRow(ctx, `SELECT work_id FROM chapters WHERE id = $1 FOR UPDATE`, rec.ChapterID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != rec.WorkID) {
		return ErrAudioNotFound
	}
	if err != nil {
		return fmt.Errorf("глава записи: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO audio_recordings (work_id, chapter_id, position, reader, s3_key, content_type,
		                              bytes, duration_ms, uploaded_by)
		VALUES ($1, $2, coalesce((SELECT max(position) FROM audio_recordings WHERE chapter_id = $2), 0) + 1,
		        $3, $4, $5, $6, $7, $8)
		RETURNING id, position, created_at`,
		rec.WorkID, rec.ChapterID, rec.Reader, rec.S3Key, rec.ContentType, rec.Bytes, rec.DurationMS, rec.UploadedBy,
	).Scan(&rec.ID, &rec.Position, &rec.CreatedAt); err != nil {
		// По имени ограничения, а не по одному коду: повтор ключа — это
		// двойная отправка той же заливки, и только его можно назвать «уже есть».
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == recordingKeyUniqueConstraint {
			return ErrAudioDuplicate
		}
		return fmt.Errorf("вставка записи: %w", err)
	}
	return tx.Commit(ctx)
}

// recordingKeyUniqueConstraint — имя, которое Postgres дал UNIQUE у
// audio_recordings.s3_key (миграция 000035).
const recordingKeyUniqueConstraint = "audio_recordings_s3_key_key"

func (r *AudioRecordingRepository) ListRecordings(ctx context.Context, workID int64) ([]models.AudioRecording, error) {
	// Тем же порядком, что audio.Playlist: объемлющая глава раньше вложенной
	// с тем же началом, иначе JSON тома и плейлист расходились бы.
	rows, err := r.pool.Query(ctx, recordingSelect+` WHERE r.work_id = $1
		ORDER BY c.start_page, c.end_page DESC, r.chapter_id, r.position`, workID)
	if err != nil {
		return nil, fmt.Errorf("записи тома: %w", err)
	}
	defer rows.Close()
	var out []models.AudioRecording
	for rows.Next() {
		x, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Recording — одна запись: ключ для подписи ссылки, глава и чтец для имени файла.
func (r *AudioRecordingRepository) Recording(ctx context.Context, id int64) (models.AudioRecording, error) {
	x, err := scanRecording(r.pool.QueryRow(ctx, recordingSelect+` WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return x, ErrAudioNotFound
	}
	return x, err
}

// lockRecordingChapter запирает строку главы записи и возвращает её id. Глава
// определяется по id записи ДО чтения самой записи: иначе перестановка,
// закоммиченная между чтением и запиранием, оставила бы прочитанную позицию
// устаревшей. Создание, правка и удаление сериализуются на одной строке главы.
func lockRecordingChapter(ctx context.Context, tx pgx.Tx, id int64) (int64, error) {
	var chapterID int64
	err := tx.QueryRow(ctx, `SELECT c.id FROM chapters c
		WHERE c.id = (SELECT chapter_id FROM audio_recordings WHERE id = $1) FOR UPDATE`, id).Scan(&chapterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrAudioNotFound
	}
	return chapterID, err
}

// UpdateRecording правит «Читает» и/или позицию. Перестановка сдвигает
// соседей одной транзакцией; уникальность (chapter_id, position) отложена до
// коммита (миграция 000035), поэтому промежуточный дубль позиции законен.
func (r *AudioRecordingRepository) UpdateRecording(ctx context.Context, id int64, reader *string, position *int) (models.AudioRecording, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return models.AudioRecording{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := lockRecordingChapter(ctx, tx, id); err != nil {
		return models.AudioRecording{}, err
	}
	cur, err := r.get(ctx, tx, id)
	if err != nil {
		return cur, err
	}
	if reader != nil {
		if _, err := tx.Exec(ctx, `UPDATE audio_recordings SET reader = $2 WHERE id = $1`, id, *reader); err != nil {
			return cur, err
		}
	}
	if position != nil {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audio_recordings WHERE chapter_id = $1`, cur.ChapterID).Scan(&n); err != nil {
			return cur, err
		}
		to := min(max(*position, 1), n)
		from := cur.Position
		switch {
		case to < from:
			_, err = tx.Exec(ctx, `UPDATE audio_recordings SET position = position + 1
				WHERE chapter_id = $1 AND position >= $2 AND position < $3`, cur.ChapterID, to, from)
		case to > from:
			_, err = tx.Exec(ctx, `UPDATE audio_recordings SET position = position - 1
				WHERE chapter_id = $1 AND position > $2 AND position <= $3`, cur.ChapterID, from, to)
		}
		if err != nil {
			return cur, err
		}
		if _, err := tx.Exec(ctx, `UPDATE audio_recordings SET position = $2 WHERE id = $1`, id, to); err != nil {
			return cur, err
		}
	}
	out, err := r.get(ctx, tx, id)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// DeleteRecording удаляет строку и смыкает позиции; объект в бакете удаляет
// вызывающий — после коммита, по возвращённому ключу.
func (r *AudioRecordingRepository) DeleteRecording(ctx context.Context, id int64) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	chapterID, err := lockRecordingChapter(ctx, tx, id)
	if err != nil {
		return "", err
	}
	var pos int
	var key string
	err = tx.QueryRow(ctx, `DELETE FROM audio_recordings WHERE id = $1
		RETURNING position, s3_key`, id).Scan(&pos, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAudioNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE audio_recordings SET position = position - 1
		WHERE chapter_id = $1 AND position > $2`, chapterID, pos); err != nil {
		return "", err
	}
	return key, tx.Commit(ctx)
}

// CountInChapterSubtree — сколько записей у главы и всех её подглав.
func (r *AudioRecordingRepository) CountInChapterSubtree(ctx context.Context, chapterID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM chapters WHERE id = $1
			UNION ALL
			SELECT c.id FROM chapters c JOIN sub ON c.parent_id = sub.id)
		SELECT count(*) FROM audio_recordings WHERE chapter_id IN (SELECT id FROM sub)`, chapterID).Scan(&n)
	return n, err
}
