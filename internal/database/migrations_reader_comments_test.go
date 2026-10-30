package database

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
)

func deleteAllReaderComments(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.reader_comments') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		if _, err := pool.Exec(ctx, `DELETE FROM reader_comments`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReaderCommentsMigrationDownRefuses(t *testing.T) {
	dsn := os.Getenv("PROOFREADER_TEST_DB_URL")
	if dsn == "" || os.Getenv("PROOFREADER_MIGRATE_TEST") != "1" {
		t.Skip("requires throwaway DB")
	}
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
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatal(err)
	}
	clearDownRefusals(ctx, t, pool)
	var uid, wid int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES('migration-comments@example.invalid','x','administrator') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO works(title,file_path,owner_id) VALUES('migration-comments','',$1) RETURNING id`, uid).Scan(&wid); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM works WHERE id=$1`, wid)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, uid)
	}()
	if _, err := pool.Exec(ctx, `INSERT INTO reader_comments(owner_id,work_id,target,body,status) VALUES($1,$2,'work','важная мысль','на_рассмотрении')`, uid, wid); err != nil {
		t.Fatal(err)
	}
	err = m.Steps(-1)
	if err == nil || !strings.Contains(err.Error(), "шаг вниз 000045") {
		t.Fatalf("неожиданный откат: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reader_comments WHERE work_id=$1`, wid).Scan(&count); err != nil || count != 1 {
		t.Fatalf("текст потерян: %d %v", count, err)
	}
	if err := m.Force(45); err != nil {
		t.Fatal(err)
	}
	deleteAllReaderComments(ctx, t, pool)
	if err := m.Steps(-1); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
}
