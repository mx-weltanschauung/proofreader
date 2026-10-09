package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
	"proofreader/pkg/slug"
)

var (
	ErrPersonNotFound = errors.New("person not found")
	ErrMergeSelf      = errors.New("cannot merge a person into itself")
)

// maxSlugAttempts — сколько суффиксов пробовать при совпадении слагов.
const maxSlugAttempts = 50

type PersonRepository struct {
	pool *pgxpool.Pool
}

func NewPersonRepository(pool *pgxpool.Pool) *PersonRepository {
	return &PersonRepository{pool: pool}
}

const personColumns = `id, name, sort_key, slug, created_at, updated_at`

func scanPerson(row pgx.Row) (*models.Person, error) {
	var p models.Person
	if err := row.Scan(&p.ID, &p.Name, &p.SortKey, &p.Slug, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPersonNotFound
		}
		return nil, err
	}
	return &p, nil
}

// Create заводит человека. Слаг — транслитерация имени; совпадение у двух
// разных людей разводится суффиксом -2, -3, … Слаг потом не меняется:
// переименование адрес не двигает.
func (r *PersonRepository) Create(ctx context.Context, p *models.Person) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.SortKey == "" {
		p.SortKey = models.PersonSortKey(p.Name)
	}
	base := slug.Text(p.Name)
	if base == "" {
		base = "avtor"
	}
	for n := 1; n <= maxSlugAttempts; n++ {
		p.Slug = base
		if n > 1 {
			p.Slug = fmt.Sprintf("%s-%d", base, n)
		}
		err := insertRow(ctx, r.pool, "persons", p.ID, []string{"name", "sort_key", "slug"},
			[]any{p.Name, p.SortKey, p.Slug}, &p.ID, &p.CreatedAt, &p.UpdatedAt)
		if isUnique(err, "persons_slug_key") {
			continue
		}
		return err
	}
	return fmt.Errorf("persons: слаг %q занят %d раз подряд", base, maxSlugAttempts)
}

func (r *PersonRepository) Update(ctx context.Context, p *models.Person) error {
	if p.SortKey == "" {
		p.SortKey = models.PersonSortKey(p.Name)
	}
	err := r.pool.QueryRow(ctx, `UPDATE persons SET name = $2, sort_key = $3 WHERE id = $1 RETURNING slug, updated_at`,
		p.ID, p.Name, p.SortKey).Scan(&p.Slug, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPersonNotFound
	}
	return err
}

func (r *PersonRepository) GetByID(ctx context.Context, id int64) (*models.Person, error) {
	return scanPerson(r.pool.QueryRow(ctx, `SELECT `+personColumns+` FROM persons WHERE id = $1`, id))
}

// Search — люди, чей ключ начинается с запроса (нормализованного тем же
// PersonSortKey); знаки шаблона экранируются ПОСЛЕ нормализации.
func (r *PersonRepository) Search(ctx context.Context, q string, limit int) ([]models.Person, error) {
	key := models.PersonSortKey(q)
	out := []models.Person{}
	if key == "" {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+personColumns+` FROM persons
		WHERE sort_key LIKE $1 ESCAPE '\' ORDER BY sort_key, id LIMIT $2`, escapeLikePattern(key)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *PersonRepository) Detail(ctx context.Context, personSlug string) (*models.PersonDetail, error) {
	p, err := scanPerson(r.pool.QueryRow(ctx, `SELECT `+personColumns+` FROM persons WHERE slug = $1`, personSlug))
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.title, coalesce(c.article_kind, ''), ac.role, w.id, w.title, w.role,
		       j.slug, j.title, ji.year, ji.label, c.start_page, c.end_page
		FROM article_credits ac
		JOIN chapters c ON c.id = ac.chapter_id
		JOIN works w ON w.id = c.work_id
		JOIN journal_issues ji ON ji.work_id = w.id
		JOIN journals j ON j.id = ji.journal_id
		WHERE ac.person_id = $1
		ORDER BY j.title, ji.year, ji.number_from, c.start_page, c.id`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d := &models.PersonDetail{Person: p, Articles: []models.PersonArticle{}}
	for rows.Next() {
		var a models.PersonArticle
		var workTitle, workRole string
		if err := rows.Scan(&a.ChapterID, &a.Title, &a.ArticleKind, &a.Role, &a.WorkID, &workTitle, &workRole,
			&a.JournalSlug, &a.JournalTitle, &a.Year, &a.Label, &a.StartPage, &a.EndPage); err != nil {
			return nil, err
		}
		a.ChapterSlug = slug.Chapter(a.Title)
		a.WorkSlug = workSlugFrom(workRole, "", nil, nil, nil, workTitle)
		d.Articles = append(d.Articles, a)
	}
	return d, rows.Err()
}

// Merge переносит подписи fromID на intoID и удаляет fromID — одной транзакцией.
func (r *PersonRepository) Merge(ctx context.Context, intoID, fromID int64) error {
	if intoID == fromID {
		return ErrMergeSelf
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM persons WHERE id IN ($1, $2)`, intoID, fromID).Scan(&n); err != nil {
		return err
	}
	if n != 2 {
		return ErrPersonNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE article_credits SET person_id = $1 WHERE person_id = $2`, intoID, fromID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM persons WHERE id = $1`, fromID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReplaceCredits заменяет подпись статьи списком целиком (позиции с 1).
func (r *PersonRepository) ReplaceCredits(ctx context.Context, chapterID int64, credits []models.CreditInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM article_credits WHERE chapter_id = $1`, chapterID); err != nil {
		return err
	}
	for i, c := range credits {
		_, err := tx.Exec(ctx, `INSERT INTO article_credits (chapter_id, position, role, printed, person_id)
			VALUES ($1, $2, $3, $4, $5)`, chapterID, i+1, c.Role, c.Printed, c.PersonID)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "article_credits_person_id_fkey" {
			return ErrPersonNotFound
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListCreditsByWork — подписи всех статей работы: глава -> строки по позиции.
func (r *PersonRepository) ListCreditsByWork(ctx context.Context, workID int64) (map[int64][]models.ArticleCredit, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ac.chapter_id, ac.position, ac.role, ac.printed, ac.person_id, coalesce(p.slug, '')
		FROM article_credits ac
		JOIN chapters c ON c.id = ac.chapter_id
		LEFT JOIN persons p ON p.id = ac.person_id
		WHERE c.work_id = $1
		ORDER BY ac.chapter_id, ac.position`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]models.ArticleCredit{}
	for rows.Next() {
		var chapterID int64
		var c models.ArticleCredit
		if err := rows.Scan(&chapterID, &c.Position, &c.Role, &c.Printed, &c.PersonID, &c.PersonSlug); err != nil {
			return nil, err
		}
		out[chapterID] = append(out[chapterID], c)
	}
	return out, rows.Err()
}
