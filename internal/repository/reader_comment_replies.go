package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"proofreader/internal/models"
)

func (r *ReaderCommentRepository) Reply(ctx context.Context, v models.CommentViewer, targetID int64, body, ip string) (*models.ReaderComment, error) {
	if targetID <= 0 {
		return nil, ErrCommentNotFound
	}
	return r.save(ctx, v, 0, models.MarginNoteInput{Body: body}, ip, 0, 0, targetID)
}

func commentRootID(ctx context.Context, tx pgx.Tx, id int64) (int64, error) {
	var rootID int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(thread_id,id) FROM reader_comments WHERE id=$1`, id).Scan(&rootID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrCommentNotFound
	}
	return rootID, err
}

// Создание, решение редактора и удаление берут корень до строки ответа.
// Тогда решение об удалении ветки видит все ответы, одобренные к этому моменту.
func lockCommentRoot(ctx context.Context, tx pgx.Tx, v models.CommentViewer, id int64) error {
	var lockedID int64
	err := tx.QueryRow(ctx, `SELECT mn.id FROM reader_comments mn WHERE mn.id=$1 AND mn.thread_id IS NULL`+
		commentClause(v)+` FOR UPDATE OF mn`, id).Scan(&lockedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCommentNotFound
	}
	return err
}

func insertReply(ctx context.Context, tx pgx.Tx, v models.CommentViewer, targetID int64, nickname, body, status string) (int64, error) {
	var rootID, workID int64
	var recipientID, chapterID, startPageID, endPageID *int64
	err := tx.QueryRow(ctx, `
		SELECT root.id,root.work_id,root.chapter_id,root.start_page_id,root.end_page_id,addressed.owner_id
		FROM reader_comments addressed
		JOIN reader_comments root ON root.id=COALESCE(addressed.thread_id,addressed.id)
		WHERE addressed.id=$1 AND addressed.published_revision>0 AND addressed.removed=''`, targetID).
		Scan(&rootID, &workID, &chapterID, &startPageID, &endPageID, &recipientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrCommentNotFound
	}
	if err != nil {
		return 0, err
	}
	// Удаление владельца или источника меняет ссылки в комментариях через
	// внешние ключи. Берём их в том же порядке, до блокировки строки корня.
	refs := &pgx.Batch{}
	refs.Queue(`SELECT id FROM users WHERE id=$1 FOR KEY SHARE`, recipientID)
	refs.Queue(`SELECT id FROM works WHERE id=$1 FOR KEY SHARE`, workID)
	refs.Queue(`SELECT id FROM chapters WHERE id=$1 FOR KEY SHARE`, chapterID)
	refs.Queue(`SELECT id FROM pages WHERE id IN ($1,$2) ORDER BY id FOR KEY SHARE`, startPageID, endPageID)
	if err := tx.SendBatch(ctx, refs).Close(); err != nil {
		return 0, err
	}
	if err := lockCommentRoot(ctx, tx, v, rootID); err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO reader_comments(owner_id,author_nickname,work_id,target,chapter_id,chapter_title,
			start_page_id,end_page_id,start_page_number,end_page_number,quote_key,quote_tail,quote_text,
			body,status,thread_id,recipient_id,reply_to_nickname)
		SELECT $3,$4,root.work_id,root.target,root.chapter_id,root.chapter_title,
			root.start_page_id,root.end_page_id,root.start_page_number,root.end_page_number,
			root.quote_key,root.quote_tail,root.quote_text,$5,$6,root.id,
			NULLIF(addressed.owner_id,$3),
			CASE WHEN addressed.thread_id IS NOT NULL THEN addressed.author_nickname END
		FROM reader_comments root JOIN reader_comments addressed ON addressed.id=$2
		WHERE root.id=$1 AND addressed.published_revision>0 AND addressed.removed=''
		RETURNING id`, rootID, targetID, v.ID, nickname, body, status).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrCommentNotFound
	}
	return id, err
}

func (r *ReaderCommentRepository) Remove(ctx context.Context, v models.CommentViewer, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rootID, err := commentRootID(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := lockCommentRoot(ctx, tx, v, rootID); err != nil {
		return err
	}
	c, err := scanComment(tx.QueryRow(ctx, commentSelect+` WHERE mn.id=$1`+commentClause(v)+` FOR UPDATE OF mn`, id))
	if err != nil {
		return err
	}
	owned := commentOwn(c, v)
	if (!owned && !v.Staff) || c.Removed != "" {
		return ErrCommentNotFound
	}
	if c.ReplyCount > 0 {
		mark := "author"
		if !owned {
			mark = "moderator"
		}
		_, err = tx.Exec(ctx, `UPDATE reader_comments SET removed=$2 WHERE id=$1`, id, mark)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM reader_comments WHERE id=$1`, id)
	}
	if err != nil {
		return err
	}
	if c.ThreadID != 0 {
		// Последний опубликованный ответ мог быть удалён после корня.
		if _, err := tx.Exec(ctx, `DELETE FROM reader_comments root WHERE root.id=$1 AND root.removed<>''
			AND NOT EXISTS (SELECT 1 FROM reader_comments reply WHERE reply.thread_id=root.id AND reply.published_revision>0)`, rootID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const publishedThread = `mn.thread_id IS NULL AND (mn.published_revision>0 OR mn.removed<>'')`

func (r *ReaderCommentRepository) Thread(ctx context.Context, v models.CommentViewer, id int64) (*models.CommentThread, error) {
	rows, err := r.pool.Query(ctx, commentSelect+`
		WHERE ((mn.id=$1 AND `+publishedThread+`) OR
		       (mn.thread_id=$1 AND (mn.published_revision>0 OR (mn.owner_id=$2 AND $2>0) OR ($3 AND mn.author_nickname=''))))`+
		commentClause(v)+` ORDER BY mn.id`, id, v.ID, v.Staff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &models.CommentThread{Replies: []*models.ReaderComment{}}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		c = commentForViewer(c, v)
		if c.ID == id {
			out.Root = c
		} else {
			out.Replies = append(out.Replies, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out.Root == nil {
		return nil, ErrCommentNotFound
	}
	return out, nil
}

const incomingReplies = ` WHERE mn.recipient_id=$1 AND mn.published_revision>0`

func (r *ReaderCommentRepository) IncomingReplies(ctx context.Context, v models.CommentViewer, limit, offset int) ([]*models.ReaderComment, error) {
	if v.ID == 0 {
		return nil, ErrCommentNotFound
	}
	rows, err := r.pool.Query(ctx, commentSelect+incomingReplies+commentClause(v)+
		` ORDER BY mn.id DESC LIMIT $2 OFFSET $3`, v.ID, limit, offset)
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
		// Уведомление содержит опубликованную редакцию и для сотрудников.
		body := c.PublishedBody
		c = commentForViewer(c, v)
		c.Body, c.PublishedBody, c.Status, c.RejectReason = body, "", "", ""
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ReaderCommentRepository) UnreadReplies(ctx context.Context, v models.CommentViewer) (int, error) {
	if v.ID == 0 {
		return 0, ErrCommentNotFound
	}
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM reader_comments mn`+incomingReplies+
		` AND mn.reply_read_at IS NULL`+commentClause(v), v.ID).Scan(&count)
	return count, err
}

func (r *ReaderCommentRepository) MarkRepliesRead(ctx context.Context, v models.CommentViewer, rootID int64, ids []int64) error {
	if v.ID == 0 {
		return ErrCommentNotFound
	}
	var visible bool
	err := r.pool.QueryRow(ctx, `WITH visible_thread AS (
		SELECT mn.id FROM reader_comments mn WHERE mn.id=$2 AND `+publishedThread+commentClause(v)+`
	), marked AS (
		UPDATE reader_comments mn SET reply_read_at=now() FROM visible_thread root
		`+incomingReplies+` AND mn.thread_id=root.id AND mn.id=ANY($3::bigint[])
			AND mn.reply_read_at IS NULL`+commentClause(v)+`
	)
	SELECT EXISTS(SELECT 1 FROM visible_thread)`, v.ID, rootID, ids).Scan(&visible)
	if err != nil {
		return err
	}
	if !visible {
		return ErrCommentNotFound
	}
	return nil
}
