package repository

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// AuthAttemptRepository считает попытки входа по отметке адреса и нику.
type AuthAttemptRepository struct {
	pool *pgxpool.Pool
}

func NewAuthAttemptRepository(pool *pgxpool.Pool) *AuthAttemptRepository {
	return &AuthAttemptRepository{pool: pool}
}

// ReserveAttempt решает, пускать ли пару (адрес, ник) к проверке пароля, и
// тем же действием записывает попытку.
//
// Счёт и запись идут в ОДНОЙ транзакции под консультативной блокировкой —
// ровно как у писем и правок полос. Двумя обращениями к пулу это «проверил —
// записал», и залп параллельных подборов проскакивает мимо предела целиком:
// замер на одноразовой базе давал восемнадцать прошедших из двадцати при
// пределе пять.
//
// Ключ — ПАРА (ip_hash, nickname_key), а не голый адрес. Причина в двух
// независимых доводах:
//
//  1. Заводить учётки ограничено тремя в сутки, но подбирающему своя учётка
//     нужна ОДНА. Если бы счёт вёлся по голому адресу, цикл выглядел бы так:
//     девять попыток подбора чужого ника, десятая — успешный вход в
//     собственную учётку, Clear стирает счёт адреса целиком, и цикл
//     повторяется сколько угодно раз в час — предел заведения учёток тут ни
//     при чём, он ограничивает число НИКОВ, а не число попыток.
//  2. Сотовые операторы держат абонентов за общим NAT: предел на голый адрес
//     был бы бюджетом на попытки входа, который читатели одной соты делят
//     между собой, — и вход по своему нику одного абонента запирал бы
//     соседа.
//
// Записывается КАЖДАЯ попытка, а не только неудачная: иначе решение и запись
// разъезжаются во времени и атомарность теряется. Удачный вход стирает счёт
// пары сам — см. Clear.
func (r *AuthAttemptRepository) ReserveAttempt(
	ctx context.Context, ipHash, nickname string, limit int, since time.Time,
) (bool, error) {
	nicknameKey := models.NormalizeNickname(nickname)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // откат после успешного Commit безвреден

	// Префикс имени таблицы в ключе обязателен: консультативные блокировки —
	// общее на всю базу пространство ключей, и без него вход, письмо и правка
	// с одного адреса вставали бы в очередь друг за другом без причины.
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("auth_attempts:" + ipHash + ":" + nicknameKey))
	lockKey := int64(hasher.Sum64())

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	// Уборка идёт здесь же: отдельного уборщика таблица не стоит.
	if _, err := tx.Exec(ctx,
		`DELETE FROM auth_attempts WHERE created_at < now() - interval '1 day'`); err != nil {
		return false, fmt.Errorf("failed to prune auth attempts: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM auth_attempts WHERE ip_hash = $1 AND nickname_key = $2 AND created_at > $3`,
		ipHash, nicknameKey, since).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to count auth attempts: %w", err)
	}
	if count >= limit {
		return false, nil
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO auth_attempts (ip_hash, nickname_key) VALUES ($1, $2)`, ipHash, nicknameKey); err != nil {
		return false, fmt.Errorf("failed to record auth attempt: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return true, nil
}

// Clear забывает попытки пары (адрес, ник) после удачного входа в этот ник:
// иначе человек, вспомнивший пароль с пятого раза, остаётся запертым на час.
// Стирает только счёт СВОЕЙ пары — счёт попыток к чужим никам с того же
// адреса трогать нельзя, иначе успешный вход в собственную учётку открывал
// бы новый круг подбора чужой (см. докблок ReserveAttempt).
func (r *AuthAttemptRepository) Clear(ctx context.Context, ipHash, nickname string) error {
	nicknameKey := models.NormalizeNickname(nickname)
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM auth_attempts WHERE ip_hash = $1 AND nickname_key = $2`, ipHash, nicknameKey); err != nil {
		return fmt.Errorf("failed to clear auth attempts: %w", err)
	}
	return nil
}
