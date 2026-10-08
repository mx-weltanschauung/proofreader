package repository

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

// UserRepository handles user data access
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository creates a new user repository
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create creates a new user
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	query := `
		INSERT INTO users (email, password_hash, role)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query, user.Email, user.PasswordHash, user.Role).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// GetByID retrieves a user by ID
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*models.User, error) {
	query := `
		SELECT id, COALESCE(email, ''), password_hash, role, nickname, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user models.User
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Nickname, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetByEmail retrieves a user by email
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `
		SELECT id, COALESCE(email, ''), password_hash, role, nickname, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var user models.User
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Nickname, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// ListStaff отдаёт сотрудников. Читатели в это окно не попадают: их заводят и
// удаляют они сами.
func (r *UserRepository) ListStaff(ctx context.Context, limit, offset int) ([]*models.User, error) {
	query := `
		SELECT id, COALESCE(email, ''), password_hash, role, nickname, created_at, updated_at
		FROM users
		WHERE role <> 'reader'
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list staff users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Nickname, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating staff users: %w", err)
	}

	return users, nil
}

// escapeLikePattern обезвреживает знаки шаблона LIKE, чтобы строка поиска
// искалась буквально.
//
// Именно NewReplacer, а не цепочка ReplaceAll: он проходит строку один раз и в
// собственный вывод не возвращается, поэтому порядок замен здесь ничего не
// решает. Цепочка из трёх ReplaceAll решала бы — экранирование косой пришлось
// бы делать первым, иначе оно удвоило бы косые, только что добавленные
// соседними заменами.
func escapeLikePattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ReaderRow — строка читательского списка для администратора: ровно то, что
// нужно, чтобы дойти от ника из жалобы до подборок. Почты здесь нет, потому
// что её у читателя нет вовсе.
type ReaderRow struct {
	ID              int64     `json:"id"`
	Nickname        string    `json:"nickname"`
	CreatedAt       time.Time `json:"created_at"`
	CollectionCount int       `json:"collections_count"`
}

// ListReaders отдаёт читателей администратору. Отдельный запрос, а не снятие
// фильтра с ListStaff: смешать оба списка значит зарастить управление
// сотрудниками читателями.
//
// Подборки считаются по СНИМКУ ника (collections.author_nickname), а не по
// owner_id: ключ из жалобы — ник, и только снимок переживает удаление учётной
// записи, у которой owner_id обнуляется.
func (r *UserRepository) ListReaders(ctx context.Context, query string, limit int) ([]ReaderRow, error) {
	// Поиск идёт по нормализованному ключу — тому же, которым Postgres держит
	// уникальность ника, — поэтому «ТЕЦ» находит «Чтец». Знаки шаблона
	// экранируются: иначе «%» показал бы всех разом.
	//
	// Порядок «сначала нормализовать, потом экранировать» обязателен и куплен
	// находкой рецензии. Нормализация NFKC сама ПОРОЖДАЕТ знаки шаблона:
	// полноширинная «％» (U+FF05) сворачивается в обычный «%», «＿» (U+FF3F) —
	// в «_». Пока экранирование шло в Go, а нормализация в SQL, такой знак
	// уезжал буквой и возвращался шаблоном, и «％» показывала всех читателей
	// разом мимо всей защиты. Поэтому нормализует теперь Go — той же
	// NormalizeNickname, что сверяет ник с закрытым списком, — и экранирует
	// уже её вывод, а SQL сравнивает с готовым ключом как есть.
	sql := `
		SELECT u.id, COALESCE(u.nickname, ''), u.created_at,
		       (SELECT count(*) FROM collections c
		         WHERE lower(normalize(c.author_nickname, NFKC)) = u.nickname_key)
		FROM users u
		WHERE u.role = 'reader'
		  AND u.nickname_key LIKE '%' || $1 || '%' ESCAPE '\'
		ORDER BY u.created_at DESC, u.id DESC
		LIMIT $2
	`

	needle := escapeLikePattern(models.NormalizeNickname(query))
	rows, err := r.pool.Query(ctx, sql, needle, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list readers: %w", err)
	}
	defer rows.Close()

	out := []ReaderRow{}
	for rows.Next() {
		var row ReaderRow
		if err := rows.Scan(&row.ID, &row.Nickname, &row.CreatedAt, &row.CollectionCount); err != nil {
			return nil, fmt.Errorf("failed to scan reader: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating readers: %w", err)
	}

	return out, nil
}

// Update updates a user
func (r *UserRepository) Update(ctx context.Context, user *models.User) error {
	query := `
		UPDATE users
		SET email = $2, role = $3
		WHERE id = $1
		RETURNING updated_at
	`

	err := r.pool.QueryRow(ctx, query, user.ID, user.Email, user.Role).
		Scan(&user.UpdatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("user not found")
		}
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// UpdatePassword updates a user's password
func (r *UserRepository) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	query := `
		UPDATE users
		SET password_hash = $2
		WHERE id = $1
	`

	result, err := r.pool.Exec(ctx, query, userID, passwordHash)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// Delete deletes a user
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM users WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// DeleteReader удаляет учётную запись ЧИТАТЕЛЯ. Условие по роли стоит в
// самом запросе, а не только в обработчике: эта дверь открыта тому, кто
// вошёл, и ошибиться в ней значит дать постороннему снять администратора
// мимо охраны последнего администратора, которая живёт в UserHandler.
//
// Всё, что читатель оставил, переживает его: подборки, правки полос и
// разборы теряют владельца (ON DELETE SET NULL), а подпись держится
// ник-снимком.
//
// Поэтому уход НЕ освобождает ник: имя уезжает в retired_nicknames, и
// читательская дверь больше его не заводит (IsNicknameRetired). Без этого
// следующий желающий занимал имя, под которым в читальне остались чужие
// тексты, и опубликованный разбор продолжал печатать «Собрал читатель ‹ник›»
// — теперь уже про живого человека, который этого не писал. Тот же довод, по
// которому ValidateNickname запрещает гомоглифы.
//
// Отставка и удаление идут ОДНОЙ транзакцией: порознь они разъедутся при
// сбое ровно так же, как счёт и запись у предела частоты — либо имя
// отставлено у живого читателя, либо строка снесена, а имя свободно, то есть
// тот же дефект, что чинится.
//
// Ключ отставки копируется из ГОТОВОГО users.nickname_key (вычисляемый
// столбец), а не считается заново выражением: сойтись они обязаны байт в
// байт, иначе «уходящий» и «УХОДЯЩИЙ» разъедутся на отставке, хотя в users
// это один читатель.
func (r *UserRepository) DeleteReader(ctx context.Context, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // откат после успешного Commit безвреден

	var nickname, nicknameKey string
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(nickname, ''), COALESCE(nickname_key, '')
		 FROM users WHERE id = $1 AND role = $2`, id, models.RoleReader).
		Scan(&nickname, &nicknameKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reader not found")
		}
		return fmt.Errorf("failed to read reader before delete: %w", err)
	}

	// Ник у читателя есть всегда (его заводит читательская дверь), но пустая
	// строка ключом отставки быть не может: она заперла бы регистрацию всем.
	if nicknameKey != "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO retired_nicknames (nickname_key, nickname) VALUES ($1, $2)
			 ON CONFLICT (nickname_key) DO NOTHING`, nicknameKey, nickname); err != nil {
			return fmt.Errorf("failed to retire nickname: %w", err)
		}
	}

	result, err := tx.Exec(ctx,
		`DELETE FROM users WHERE id = $1 AND role = $2`, id, models.RoleReader)
	if err != nil {
		return fmt.Errorf("failed to delete reader: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("reader not found")
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// IsNicknameRetired сообщает, числится ли имя за ушедшим читателем.
//
// Сравнение — тем же выражением, что у GetByNickname и у вычисляемого
// столбца users.nickname_key: «Уходящий», «уходящий» и «УХОДЯЩИЙ» это одно
// имя, и отставка обязана накрывать все три. Вызывающий передаёт уже
// триммленную строку (SQL normalize() пробелов не режет — см. докстроку
// models.NormalizeNickname).
func (r *UserRepository) IsNicknameRetired(ctx context.Context, nickname string) (bool, error) {
	var retired bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM retired_nicknames
		               WHERE nickname_key = lower(normalize($1, NFKC)))`, nickname).Scan(&retired)
	if err != nil {
		return false, fmt.Errorf("failed to check retired nickname: %w", err)
	}
	return retired, nil
}

// Count returns the total number of users
func (r *UserRepository) Count(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM users`

	var count int
	err := r.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}

	return count, nil
}

// CountByRole returns the number of users with the given role.
func (r *UserRepository) CountByRole(ctx context.Context, role models.UserRole) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = $1`, role).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users by role: %w", err)
	}
	return count, nil
}

// GetByNickname ищет читателя по нику. Сравнение идёт по нормализованной
// форме — тому же вычисляемому столбцу, что держит уникальность, — поэтому
// «Чтец» и «чтец» это один и тот же читатель.
func (r *UserRepository) GetByNickname(ctx context.Context, nickname string) (*models.User, error) {
	query := `
		SELECT id, COALESCE(email, ''), password_hash, role, nickname, created_at, updated_at
		FROM users
		WHERE nickname_key = lower(normalize($1, NFKC))
	`

	var user models.User
	err := r.pool.QueryRow(ctx, query, nickname).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Nickname,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to get user by nickname: %w", err)
	}
	return &user, nil
}

// CreateReaderWithinLimit заводит читателя, если предел заведения учёток по
// отметке адреса ещё не выбран.
//
// Тот же рельс, что у писем и правок: счёт и вставка — одна транзакция под
// консультативной блокировкой. Ник бесплатен ровно так же, как был бесплатен
// билет, поэтому предел по читателю не защищает ни от чего — считаем по
// адресу.
func (r *UserRepository) CreateReaderWithinLimit(
	ctx context.Context, user *models.User, limit int, since time.Time,
) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("users:" + user.SignupIPHash))
	lockKey := int64(hasher.Sum64())

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey); err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE signup_ip_hash = $1 AND created_at > $2`,
		user.SignupIPHash, since).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to count recent signups: %w", err)
	}
	if count >= limit {
		return false, nil
	}

	query := `
		INSERT INTO users (email, password_hash, role, nickname, signup_ip_hash)
		VALUES (NULL, $1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`
	if err := tx.QueryRow(ctx, query,
		user.PasswordHash, user.Role, user.Nickname, user.SignupIPHash,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return false, fmt.Errorf("failed to create reader: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}
	return true, nil
}
