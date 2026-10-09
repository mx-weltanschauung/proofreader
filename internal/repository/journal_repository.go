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

var (
	ErrJournalNotFound  = errors.New("journal not found")
	ErrJournalSlugTaken = errors.New("journal slug already taken")
	ErrIssueTaken       = errors.New("journal issue already exists")
	ErrIssueNotFound    = errors.New("journal issue not found")
)

// JournalRepository — журналы и их номера.
type JournalRepository struct {
	pool *pgxpool.Pool
}

func NewJournalRepository(pool *pgxpool.Pool) *JournalRepository {
	return &JournalRepository{pool: pool}
}

const journalColumns = `id, slug, title, subtitle, description, created_at, updated_at`

func scanJournal(row pgx.Row) (*models.Journal, error) {
	var j models.Journal
	if err := row.Scan(&j.ID, &j.Slug, &j.Title, &j.Subtitle, &j.Description, &j.CreatedAt, &j.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJournalNotFound
		}
		return nil, err
	}
	return &j, nil
}

func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func (r *JournalRepository) Create(ctx context.Context, j *models.Journal) error {
	cols := []string{"slug", "title", "subtitle", "description"}
	args := []any{j.Slug, j.Title, j.Subtitle, j.Description}
	err := insertRow(ctx, r.pool, "journals", j.ID, cols, args, &j.ID, &j.CreatedAt, &j.UpdatedAt)
	if isUnique(err, "journals_slug_key") {
		return fmt.Errorf("%w: %s", ErrJournalSlugTaken, j.Slug)
	}
	return err
}

func (r *JournalRepository) Update(ctx context.Context, j *models.Journal) error {
	err := r.pool.QueryRow(ctx, `UPDATE journals SET slug = $2, title = $3, subtitle = $4, description = $5
		WHERE id = $1 RETURNING updated_at`, j.ID, j.Slug, j.Title, j.Subtitle, j.Description).Scan(&j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJournalNotFound
	}
	if isUnique(err, "journals_slug_key") {
		return fmt.Errorf("%w: %s", ErrJournalSlugTaken, j.Slug)
	}
	return err
}

func (r *JournalRepository) GetByID(ctx context.Context, id int64) (*models.Journal, error) {
	return scanJournal(r.pool.QueryRow(ctx, `SELECT `+journalColumns+` FROM journals WHERE id = $1`, id))
}

// List — журналы с числом и годами загруженных номеров, по заглавию.
func (r *JournalRepository) List(ctx context.Context) ([]models.JournalSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT j.id, j.slug, j.title, j.subtitle, j.description, j.created_at, j.updated_at,
		       count(ji.id), min(ji.year), max(ji.year)
		FROM journals j LEFT JOIN journal_issues ji ON ji.journal_id = j.id
		GROUP BY j.id ORDER BY j.title, j.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.JournalSummary{}
	for rows.Next() {
		var s models.JournalSummary
		if err := rows.Scan(&s.ID, &s.Slug, &s.Title, &s.Subtitle, &s.Description, &s.CreatedAt, &s.UpdatedAt,
			&s.IssuesTotal, &s.YearFrom, &s.YearTo); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Detail — журнал и номера по годам; порядок — год, затем первый номер.
func (r *JournalRepository) Detail(ctx context.Context, slug string) (*models.JournalDetail, error) {
	j, err := scanJournal(r.pool.QueryRow(ctx, `SELECT `+journalColumns+` FROM journals WHERE slug = $1`, slug))
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT ji.id, ji.year, ji.label, ji.months, w.id, w.title, w.role
		FROM journal_issues ji JOIN works w ON w.id = ji.work_id
		WHERE ji.journal_id = $1 ORDER BY ji.year, ji.number_from`, j.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d := &models.JournalDetail{Journal: j, Years: []models.JournalYear{}}
	for rows.Next() {
		var (
			ref         models.JournalIssueRef
			year        int
			title, role string
		)
		if err := rows.Scan(&ref.ID, &year, &ref.Label, &ref.Months, &ref.WorkID, &title, &role); err != nil {
			return nil, err
		}
		// Слаг работы номера — тем же правилом, что везде (work_slug.go).
		ref.WorkSlug = workSlugFrom(role, "", nil, nil, nil, title)
		if n := len(d.Years); n == 0 || d.Years[n-1].Year != year {
			d.Years = append(d.Years, models.JournalYear{Year: year})
		}
		last := &d.Years[len(d.Years)-1]
		last.Issues = append(last.Issues, ref)
	}
	return d, rows.Err()
}

// CreateIssue заводит работу номера и строку номера одной транзакцией;
// у обеих допустим явный id. Роль и пустые издание/родитель работы
// выставляются здесь, а не вызывающим.
func (r *JournalRepository) CreateIssue(ctx context.Context, issue *models.JournalIssue, work *models.Work) error {
	work.Role = models.WorkRoleJournalIssue
	work.EditionID, work.ParentWorkID, work.VolumeNumber, work.VolumePart = nil, nil, nil, nil
	work.PageOffset = 0
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cols, args := prepareWorkInsert(work)
	if err := insertRowTx(ctx, tx, "works", work.ID, cols, args, &work.ID, &work.CreatedAt, &work.UpdatedAt); err != nil {
		return fmt.Errorf("failed to create issue work: %w", err)
	}
	issue.WorkID = work.ID
	err = insertRowTx(ctx, tx, "journal_issues", issue.ID,
		[]string{"journal_id", "year", "number_from", "number_to", "label", "months", "work_id"},
		[]any{issue.JournalID, issue.Year, issue.NumberFrom, issue.NumberTo, issue.Label, issue.Months, issue.WorkID},
		&issue.ID, &issue.CreatedAt, &issue.UpdatedAt)
	if isUnique(err, "journal_issues_journal_year_number_key") {
		return fmt.Errorf("%w: %d, № %s", ErrIssueTaken, issue.Year, issue.Label)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *JournalRepository) GetIssue(ctx context.Context, id int64) (*models.JournalIssue, error) {
	var i models.JournalIssue
	err := r.pool.QueryRow(ctx, `SELECT id, journal_id, year, number_from, number_to, label, months, work_id,
		created_at, updated_at FROM journal_issues WHERE id = $1`, id).Scan(
		&i.ID, &i.JournalID, &i.Year, &i.NumberFrom, &i.NumberTo, &i.Label, &i.Months, &i.WorkID, &i.CreatedAt, &i.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// UpdateIssue правит координаты номера и заглавие его работы одной
// транзакцией: заглавие выводится из координат, и разойтись им нельзя.
func (r *JournalRepository) UpdateIssue(ctx context.Context, issue *models.JournalIssue, title string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `UPDATE journal_issues SET year = $2, number_from = $3, number_to = $4, label = $5, months = $6
		WHERE id = $1 RETURNING work_id, updated_at`, issue.ID, issue.Year, issue.NumberFrom, issue.NumberTo,
		issue.Label, issue.Months).Scan(&issue.WorkID, &issue.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIssueNotFound
	}
	if isUnique(err, "journal_issues_journal_year_number_key") {
		return fmt.Errorf("%w: %d, № %s", ErrIssueTaken, issue.Year, issue.Label)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE works SET title = $2 WHERE id = $1`, issue.WorkID, title); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IssueForWork — журнальные координаты работы; (nil, nil), если работа не номер.
func (r *JournalRepository) IssueForWork(ctx context.Context, workID int64) (*models.WorkJournalIssue, error) {
	var v models.WorkJournalIssue
	err := r.pool.QueryRow(ctx, `
		SELECT ji.id, j.id, j.slug, j.title, ji.year, ji.label, ji.months
		FROM journal_issues ji JOIN journals j ON j.id = ji.journal_id
		WHERE ji.work_id = $1`, workID).Scan(
		&v.IssueID, &v.JournalID, &v.JournalSlug, &v.JournalTitle, &v.Year, &v.Label, &v.Months)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}
