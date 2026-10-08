package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrIDTaken — явный id уже занят другой строкой.
var ErrIDTaken = errors.New("id already taken")

// placeholders печатает "$from, $from+1, …" на n мест.
func placeholders(from, n int) string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("$%d", from+i)
	}
	return strings.Join(out, ", ")
}

// insertRow вставляет строку и сканирует id, created_at, updated_at в dest.
//
// id > 0 — явный id: публикатор совмещает id томов и изданий с локальными
// (спека 2026-10-03-local-scans-working-set, часть 3). Строка ложится ровно
// под этот id, а последовательность таблицы подтягивается до max(id) и назад
// не ходит — иначе следующая вставка без id получила бы уже занятое число.
// Обе записи — одной транзакцией. Занятый id — ErrIDTaken: публикатор на нём
// отказывает, а не повторяет.
//
// table — имя из кода репозитория, не ввод: подставляется в текст запроса.
func insertRow(ctx context.Context, pool *pgxpool.Pool, table string, id int64,
	cols []string, args []any, dest ...any) error {
	if id <= 0 {
		q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) RETURNING id, created_at, updated_at`,
			table, strings.Join(cols, ", "), placeholders(1, len(cols)))
		return pool.QueryRow(ctx, q, args...).Scan(dest...)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit это no-op
	q := fmt.Sprintf(`INSERT INTO %s (id, %s) VALUES ($1, %s) RETURNING id, created_at, updated_at`,
		table, strings.Join(cols, ", "), placeholders(2, len(cols)))
	if err := tx.QueryRow(ctx, q, append([]any{id}, args...)...).Scan(dest...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == table+"_pkey" {
			return fmt.Errorf("%w: %s.id = %d", ErrIDTaken, table, id)
		}
		return err
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(
		`SELECT setval('%[1]s_id_seq', greatest(last_value, (SELECT max(id) FROM %[1]s))) FROM %[1]s_id_seq`,
		table)); err != nil {
		return fmt.Errorf("подтяжка последовательности %s_id_seq: %w", table, err)
	}
	return tx.Commit(ctx)
}
