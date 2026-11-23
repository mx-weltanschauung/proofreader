package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommentRepliesMigrationPreservesOldComments(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" || os.Getenv("PROOFREADER_MIGRATE_TEST") != "1" {
		t.Skip("requires throwaway DB")
	}
	const previous = 53
	const replies = 55
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	m, err := migrate.New("file://migrations", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Migrate(previous); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatal(err)
	}
	var uid, wid, cid int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role)
		VALUES('migration-replies@example.invalid','x','administrator') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, uid) }()
	if err := pool.QueryRow(ctx, `INSERT INTO works(title,file_path,owner_id)
		VALUES('migration-replies','',$1) RETURNING id`, uid).Scan(&wid); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM works WHERE id=$1`, wid) }()
	if err := pool.QueryRow(ctx, `INSERT INTO reader_comments(owner_id,work_id,target,body,status,revision,published_body,published_revision)
		VALUES($1,$2,'work','поздняя правка','на_рассмотрении',2,'первая редакция',1) RETURNING id`, uid, wid).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO reader_comment_revisions(comment_id,revision,body,status)
		VALUES($1,1,'первая редакция','одобрено'),($1,2,'поздняя правка','на_рассмотрении')`, cid); err != nil {
		t.Fatal(err)
	}
	checkOld := func() {
		t.Helper()
		var unchanged bool
		err := pool.QueryRow(ctx, `SELECT body='поздняя правка' AND status='на_рассмотрении' AND revision=2
			AND published_body='первая редакция' AND published_revision=1
			AND (SELECT count(*) FROM reader_comment_revisions WHERE comment_id=$1)=2
			FROM reader_comments WHERE id=$1`, cid).Scan(&unchanged)
		if err != nil || !unchanged {
			t.Fatalf("старый комментарий или его история изменились: %v", err)
		}
	}
	if err := m.Migrate(replies); err != nil {
		t.Fatal(err)
	}
	checkOld()
	var root bool
	err = pool.QueryRow(ctx, `SELECT thread_id IS NULL AND recipient_id IS NULL
		AND reply_to_nickname IS NULL AND reply_read_at IS NULL AND removed=''
		FROM reader_comments WHERE id=$1`, cid).Scan(&root)
	if err != nil || !root {
		t.Fatalf("старый комментарий получил неверные поля ветки: %v", err)
	}
	if err := m.Migrate(previous); err != nil {
		t.Fatal(err)
	}
	checkOld()
	if err := m.Migrate(replies); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO reader_comments(owner_id,work_id,target,body,status,thread_id)
		VALUES($1,$2,'work','ответ','на_рассмотрении',$3)`, uid, wid, cid); err != nil {
		t.Fatal(err)
	}
	if err := m.Steps(-1); err == nil || !strings.Contains(err.Error(), "откат 000055") {
		t.Fatalf("откат с ответом: %v", err)
	}
	if err := m.Force(replies); err != nil {
		t.Fatal(err)
	}
	checkOld()
}
