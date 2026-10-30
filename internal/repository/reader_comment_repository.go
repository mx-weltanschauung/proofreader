package repository

import (
	"context"
	"errors"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"proofreader/internal/models"
)

var (
	ErrCommentNotFound = errors.New("comment not found")
	ErrCommentConflict = errors.New("comment revision conflict")
	ErrCommentRate     = errors.New("comment rate exceeded")
	ErrCommentBanned   = errors.New("commenting banned")
)

type ReaderCommentRepository struct{ pool *pgxpool.Pool }

func NewReaderCommentRepository(pool *pgxpool.Pool) *ReaderCommentRepository {
	return &ReaderCommentRepository{pool: pool}
}

// Якори, адреса и контекст читаются тем же запросом, что у личных заметок.
var commentSelect = strings.Replace(strings.Replace(marginNoteSelect, "mn.owner_id,", "COALESCE(mn.owner_id,0),", 1), "\n\tFROM margin_notes mn", `, mn.author_nickname, mn.status, mn.revision, mn.published_body,
 mn.published_revision, mn.reject_reason, mn.edited_at
 FROM reader_comments mn`, 1)

// Цитата может пересекать закрытую полосу в середине, а не только концами.
const commentOpen = marginNoteOpen + ` AND NOT EXISTS (
 SELECT 1 FROM closed_pages cp WHERE cp.work_id=mn.work_id
 AND cp.page_number BETWEEN COALESCE((SELECT page_number FROM pages WHERE id=mn.start_page_id),mn.start_page_number)
 AND COALESCE((SELECT page_number FROM pages WHERE id=mn.end_page_id),mn.end_page_number))`

type commentScanner struct {
	row   rowScanner
	extra []any
}

func (s commentScanner) Scan(dest ...any) error { return s.row.Scan(append(dest, s.extra...)...) }
func scanComment(row rowScanner) (*models.ReaderComment, error) {
	c := &models.ReaderComment{}
	n, err := scanMarginNote(commentScanner{row, []any{&c.AuthorNickname, &c.Status, &c.Revision, &c.PublishedBody, &c.PublishedRevision, &c.RejectReason, &c.EditedAt}})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCommentNotFound
		}
		return nil, err
	}
	c.MarginNote = *n
	return c, nil
}
func commentClause(v models.CommentViewer) string {
	if v.Staff {
		return ""
	}
	return commentOpen
}
func commentOwn(c *models.ReaderComment, v models.CommentViewer) bool {
	return v.ID > 0 && (c.OwnerID == v.ID || (v.Staff && c.AuthorNickname == ""))
}
func commentForViewer(c *models.ReaderComment, v models.CommentViewer) *models.ReaderComment {
	c.CanEdit = commentOwn(c, v)
	c.CanManage = c.CanEdit || v.Staff
	if !c.CanEdit && !v.Staff {
		c.Body = c.PublishedBody
		c.Revision = c.PublishedRevision
		c.Status = ""
		c.PublishedBody = ""
		c.PublishedRevision = 0
		c.RejectReason = ""
		c.UpdatedAt = c.CreatedAt
		if c.EditedAt != nil {
			c.UpdatedAt = *c.EditedAt
		}
	}
	return c
}
func (r *ReaderCommentRepository) Get(ctx context.Context, v models.CommentViewer, id int64) (*models.ReaderComment, error) {
	c, err := scanComment(r.pool.QueryRow(ctx, commentSelect+` WHERE mn.id=$1`+commentClause(v), id))
	if err != nil {
		return nil, err
	}
	if c.PublishedRevision == 0 && !commentOwn(c, v) && !v.Staff {
		return nil, ErrCommentNotFound
	}
	return commentForViewer(c, v), nil
}

// List: по работе, свои или очередь. Публичная часть включается явно.
func (r *ReaderCommentRepository) List(ctx context.Context, v models.CommentViewer, workID int64, public, mine, queue bool, limit, offset int) ([]*models.ReaderComment, error) {
	where := ` WHERE ($1::bigint=0 OR mn.work_id=$1)` + commentClause(v)
	if queue {
		if !v.Staff {
			return nil, ErrCommentNotFound
		}
		where += ` AND mn.status='на_рассмотрении'`
	} else if mine {
		where += ` AND (mn.owner_id=$2 AND $2>0 OR ($3 AND mn.author_nickname=''))`
	} else {
		where += ` AND ((mn.owner_id=$2 AND $2>0) OR ($3 AND mn.author_nickname='') OR ($4 AND mn.published_revision>0))`
	}
	// Все параметры присутствуют независимо от ветки WHERE (pgx проверяет их число).
	where += ` AND ($2::bigint>=0) AND ($3::boolean OR NOT $3) AND ($4::boolean OR NOT $4)`
	rows, err := r.pool.Query(ctx, commentSelect+where+` ORDER BY mn.created_at,mn.id LIMIT $5 OFFSET $6`, workID, v.ID, v.Staff, public, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.ReaderComment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, commentForViewer(c, v))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Только очередь несёт сведения о блокировке и числе отказов.
	if queue {
		for _, c := range out {
			if c.OwnerID > 0 {
				id := c.OwnerID
				c.AuthorID = &id
				if err := r.pool.QueryRow(ctx, `SELECT comments_rejections FROM users WHERE id=$1`, id).Scan(&c.Rejections); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}

func commentLock(ctx context.Context, tx pgx.Tx, key string) error {
	h := fnv.New64a()
	_, _ = h.Write([]byte("reader_comments:" + key))
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(h.Sum64()))
	return err
}

// Save держит живую строку пользователя до конца: ban и отправка упорядочены,
// старый JWT запрет не обходит. История и счёт частоты пишутся атомарно.
func (r *ReaderCommentRepository) Save(ctx context.Context, v models.CommentViewer, id int64, in models.MarginNoteInput, ip string, noteID int64, expected int) (*models.ReaderComment, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var banned bool
	var nickname string
	if err := tx.QueryRow(ctx, `SELECT comments_banned,COALESCE(nickname,'') FROM users WHERE id=$1 FOR NO KEY UPDATE`, v.ID).Scan(&banned, &nickname); err != nil {
		return nil, err
	}
	if banned && !v.Staff {
		return nil, ErrCommentBanned
	}
	if !v.Staff {
		if err := commentLock(ctx, tx, ip); err != nil {
			return nil, err
		}
		var account, address int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE owner_id=$1 AND created_at>now()-interval '1 hour'),count(*) FILTER (WHERE ip_hash=$2) FROM reader_comment_submissions WHERE created_at>now()-interval '24 hours' AND (owner_id=$1 OR ip_hash=$2)`, v.ID, ip).Scan(&account, &address); err != nil {
			return nil, err
		}
		if account >= 10 || address >= 30 {
			return nil, ErrCommentRate
		}
	}
	status := "на_рассмотрении"
	if v.Staff {
		status = "одобрено"
		nickname = ""
	}
	revision := 1
	if id == 0 {
		if noteID > 0 {
			// Сотрудник тоже не может превратить чужую личную заметку.
			note, err := scanMarginNote(tx.QueryRow(ctx, marginNoteSelect+` WHERE mn.id=$1 AND mn.owner_id=$2`+marginOpenClause(v.Staff)+` FOR UPDATE OF mn`, noteID, v.ID))
			if err != nil {
				return nil, ErrCommentNotFound
			}
			in.WorkID = note.WorkID
			// Якорь копируется SQL: даже перезалитая полоса сохраняет снимки.
			err = tx.QueryRow(ctx, `INSERT INTO reader_comments(owner_id,author_nickname,work_id,target,chapter_id,chapter_title,start_page_id,end_page_id,start_page_number,end_page_number,quote_key,quote_tail,quote_text,body,status)
    SELECT owner_id,$3,work_id,target,chapter_id,chapter_title,start_page_id,end_page_id,start_page_number,end_page_number,quote_key,quote_tail,quote_text,$4,$5 FROM margin_notes WHERE id=$1 AND owner_id=$2 RETURNING id`, noteID, v.ID, nickname, in.Body, status).Scan(&id)
			if err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM margin_notes WHERE id=$1`, noteID); err != nil {
				return nil, err
			}
		} else {
			err = tx.QueryRow(ctx, `INSERT INTO reader_comments(owner_id,author_nickname,work_id,target,chapter_id,chapter_title,start_page_id,end_page_id,start_page_number,end_page_number,quote_key,quote_tail,quote_text,body,status)
    VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`, v.ID, nickname, in.WorkID, in.Target, in.ChapterID, in.ChapterTitle, in.StartPageID, in.EndPageID, in.StartPageNumber, in.EndPageNumber, in.QuoteKey, in.QuoteTail, in.QuoteText, in.Body, status).Scan(&id)
			if err != nil {
				return nil, err
			}
		}
	} else {
		c, err := scanComment(tx.QueryRow(ctx, commentSelect+` WHERE mn.id=$1`+commentClause(v)+` FOR UPDATE OF mn`, id))
		if err != nil {
			return nil, err
		}
		if !commentOwn(c, v) {
			return nil, ErrCommentNotFound
		}
		if c.Revision != expected {
			return nil, ErrCommentConflict
		}
		revision = c.Revision + 1
		if _, err := tx.Exec(ctx, `UPDATE reader_comments SET body=$2,status=$3,revision=$4,reject_reason='',updated_at=now() WHERE id=$1`, id, in.Body, status, revision); err != nil {
			return nil, err
		}
	}
	var visible bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reader_comments mn WHERE mn.id=$1`+commentClause(v)+`)`, id).Scan(&visible); err != nil {
		return nil, err
	}
	if !visible {
		return nil, ErrCommentNotFound
	}
	if v.Staff {
		if _, err := tx.Exec(ctx, `UPDATE reader_comments SET published_body=body,published_revision=revision,edited_at=CASE WHEN published_revision>0 THEN now() ELSE NULL END WHERE id=$1`, id); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO reader_comment_revisions(comment_id,revision,body,status) VALUES($1,$2,$3,$4)`, id, revision, in.Body, status); err != nil {
		return nil, err
	}
	if !v.Staff {
		if _, err := tx.Exec(ctx, `INSERT INTO reader_comment_submissions(owner_id,ip_hash) VALUES($1,$2)`, v.ID, ip); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, v, id)
}

func (r *ReaderCommentRepository) Review(ctx context.Context, v models.CommentViewer, id int64, revision int, approve bool, reason string) error {
	if !v.Staff {
		return ErrCommentNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Порядок блокировок одинаков с Save и Ban: пользователь, комментарий.
	var owner *int64
	if err := tx.QueryRow(ctx, `SELECT owner_id FROM reader_comments WHERE id=$1`, id).Scan(&owner); err != nil {
		return ErrCommentConflict
	}
	if owner != nil {
		if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, *owner); err != nil {
			return err
		}
	}
	status := "отклонено"
	if approve {
		status = "одобрено"
		reason = ""
	}
	tag, err := tx.Exec(ctx, `UPDATE reader_comments SET status=$3,reject_reason=$4,
 published_body=CASE WHEN $5 THEN body ELSE published_body END,
 edited_at=CASE WHEN $5 AND published_revision>0 THEN now() ELSE edited_at END,
 published_revision=CASE WHEN $5 THEN revision ELSE published_revision END
 WHERE id=$1 AND revision=$2 AND status='на_рассмотрении'`, id, revision, status, reason, approve)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCommentConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE reader_comment_revisions SET status=$3,reason=$4,moderator_id=$5 WHERE comment_id=$1 AND revision=$2`, id, revision, status, reason, v.ID); err != nil {
		return err
	}
	if !approve && owner != nil {
		if _, err := tx.Exec(ctx, `UPDATE users SET comments_rejections=comments_rejections+1 WHERE id=$1`, *owner); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ReaderCommentRepository) History(ctx context.Context, v models.CommentViewer, id int64) ([]models.CommentRevision, error) {
	c, err := r.Get(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if !c.CanEdit && !v.Staff {
		return nil, ErrCommentNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT revision,body,status,reason,created_at FROM reader_comment_revisions WHERE comment_id=$1 ORDER BY revision`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.CommentRevision{}
	for rows.Next() {
		var rev models.CommentRevision
		if err := rows.Scan(&rev.Revision, &rev.Body, &rev.Status, &rev.Reason, &rev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

// Без ответов (этап 2) удаление окончательное; очередь отзывается вместе с ним.
func (r *ReaderCommentRepository) Remove(ctx context.Context, v models.CommentViewer, id int64) error {
	c, err := r.Get(ctx, v, id)
	if err != nil {
		return err
	}
	if !c.CanManage {
		return ErrCommentNotFound
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM reader_comments mn WHERE mn.id=$1`+commentClause(v), id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCommentNotFound
	}
	return nil
}

// ToNote доступно только автору ещё не опубликованного комментария.
func (r *ReaderCommentRepository) ToNote(ctx context.Context, v models.CommentViewer, id int64) (*models.MarginNote, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Сначала пользователь, затем комментарий: один порядок с Save/Review/Ban.
	// NO KEY UPDATE совместим с KEY SHARE внешних ключей личных заметок.
	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, v.ID); err != nil {
		return nil, err
	}

	c, err := scanComment(tx.QueryRow(ctx, commentSelect+` WHERE mn.id=$1 AND mn.owner_id=$2`+commentClause(v)+` FOR UPDATE OF mn`, id, v.ID))
	if err != nil {
		return nil, err
	}
	if c.PublishedRevision > 0 {
		return nil, ErrCommentConflict
	}
	// Потолок общий с личными заметками; та же блокировка исключает гонку.
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("margin_notes:" + strconv.FormatInt(v.ID, 10)))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(hasher.Sum64())); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM margin_notes WHERE owner_id=$1`, v.ID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= models.MarginNotesPerOwner {
		return nil, ErrMarginNoteLimit
	}
	var noteID int64
	err = tx.QueryRow(ctx, `INSERT INTO margin_notes(owner_id,work_id,target,chapter_id,chapter_title,start_page_id,end_page_id,start_page_number,end_page_number,quote_key,quote_tail,quote_text,body)
 SELECT owner_id,work_id,target,chapter_id,chapter_title,start_page_id,end_page_id,start_page_number,end_page_number,quote_key,quote_tail,quote_text,body FROM reader_comments WHERE id=$1 RETURNING id`, id).Scan(&noteID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM reader_comments WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return NewMarginNoteRepository(r.pool).get(ctx, noteID)
}

func (r *ReaderCommentRepository) Ban(ctx context.Context, v models.CommentViewer, id int64) error {
	if !v.Staff {
		return ErrCommentNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var owner int64
	if err := tx.QueryRow(ctx, `SELECT owner_id FROM reader_comments WHERE id=$1 AND author_nickname<>'' AND owner_id IS NOT NULL`, id).Scan(&owner); err != nil {
		return ErrCommentNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET comments_banned=true WHERE id=$1 AND role='reader'`, owner); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `WITH rejected AS (UPDATE reader_comments SET status='отклонено',reject_reason='Комментирование запрещено редакцией' WHERE owner_id=$1 AND status='на_рассмотрении' RETURNING id,revision)
 UPDATE reader_comment_revisions rv SET status='отклонено',reason='Комментирование запрещено редакцией',moderator_id=$2 FROM rejected r WHERE rv.comment_id=r.id AND rv.revision=r.revision`, owner, v.ID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET comments_rejections=comments_rejections+$2 WHERE id=$1`, owner, tag.RowsAffected()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *ReaderCommentRepository) Unban(ctx context.Context, v models.CommentViewer, owner int64) error {
	if !v.Admin {
		return ErrCommentNotFound
	}
	tag, err := r.pool.Exec(ctx, `UPDATE users SET comments_banned=false WHERE id=$1`, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCommentNotFound
	}
	return nil
}

func (r *ReaderCommentRepository) PendingCount(ctx context.Context, v models.CommentViewer) (int, error) {
	if !v.Staff {
		return 0, ErrCommentNotFound
	}
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM reader_comments WHERE status='на_рассмотрении'`).Scan(&count)
	return count, err
}
func (r *ReaderCommentRepository) Banned(ctx context.Context, v models.CommentViewer) ([]models.BannedCommenter, error) {
	if !v.Admin {
		return nil, ErrCommentNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT id,COALESCE(nickname,'') FROM users WHERE comments_banned ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.BannedCommenter{}
	for rows.Next() {
		var u models.BannedCommenter
		if err := rows.Scan(&u.ID, &u.Nickname); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
