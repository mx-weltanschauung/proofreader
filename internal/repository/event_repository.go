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
	ErrEventOrgNotFound  = errors.New("event organization not found")
	ErrEventKindNotFound = errors.New("event kind not found")
	ErrEventNotFound     = errors.New("event not found")
	ErrEventSlugTaken    = errors.New("event slug already taken")
	ErrEventIDTaken      = errors.New("event id already taken")
	ErrEventKindInUse    = errors.New("event kind has events")
	ErrAgendaTarget      = errors.New("agenda target is invalid")
	ErrChapterNotInWork  = errors.New("chapter not found in work")
)

// EventRepository — организации, виды, события и их повестки.
type EventRepository struct {
	pool *pgxpool.Pool
}

func NewEventRepository(pool *pgxpool.Pool) *EventRepository {
	return &EventRepository{pool: pool}
}

// eventIDErr сводит общий отказ явного id к ошибке событий.
func eventIDErr(err error) error {
	if errors.Is(err, ErrIDTaken) {
		return fmt.Errorf("%w: %v", ErrEventIDTaken, err)
	}
	return err
}

func isFKViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}

func (r *EventRepository) CreateOrganization(ctx context.Context, o *models.EventOrganization) error {
	var created, updated any
	cols := []string{"slug", "title", "description", "position"}
	args := []any{o.Slug, o.Title, o.Description, o.Position}
	err := insertRow(ctx, r.pool, "event_organizations", o.ID, cols, args, &o.ID, &created, &updated)
	if isUnique(err, "event_organizations_slug_key") {
		return fmt.Errorf("%w: %s", ErrEventSlugTaken, o.Slug)
	}
	return eventIDErr(err)
}

// UpdateOrganization слаг не пишет: адрес наружу не протухает.
func (r *EventRepository) UpdateOrganization(ctx context.Context, o *models.EventOrganization) error {
	tag, err := r.pool.Exec(ctx, `UPDATE event_organizations
		SET title = $2, description = $3, position = $4, updated_at = NOW() WHERE id = $1`,
		o.ID, o.Title, o.Description, o.Position)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEventOrgNotFound
	}
	return nil
}

// CreateKind: у вида нет временных меток, поэтому insertRow не годится.
func (r *EventRepository) CreateKind(ctx context.Context, k *models.EventKind) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit это no-op
	if k.ID > 0 {
		err = tx.QueryRow(ctx, `INSERT INTO event_kinds (id, organization_id, title, position)
			VALUES ($1, $2, $3, $4) RETURNING id`, k.ID, k.OrganizationID, k.Title, k.Position).Scan(&k.ID)
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO event_kinds (organization_id, title, position)
			VALUES ($1, $2, $3) RETURNING id`, k.OrganizationID, k.Title, k.Position).Scan(&k.ID)
	}
	if err != nil {
		if isUnique(err, "event_kinds_pkey") {
			return fmt.Errorf("%w: event_kinds.id = %d", ErrEventIDTaken, k.ID)
		}
		if isFKViolation(err, "event_kinds_organization_id_fkey") {
			return ErrEventOrgNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT setval('event_kinds_id_seq',
		greatest(last_value, (SELECT max(id) FROM event_kinds))) FROM event_kinds_id_seq`); err != nil {
		return fmt.Errorf("подтяжка последовательности event_kinds_id_seq: %w", err)
	}
	return tx.Commit(ctx)
}

// UpdateKind: организацию вида не меняет.
func (r *EventRepository) UpdateKind(ctx context.Context, k *models.EventKind) error {
	tag, err := r.pool.Exec(ctx, `UPDATE event_kinds SET title = $2, position = $3 WHERE id = $1`,
		k.ID, k.Title, k.Position)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEventKindNotFound
	}
	return nil
}

func (r *EventRepository) DeleteKind(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM event_kinds WHERE id = $1`, id)
	if isFKViolation(err, "events_kind_id_fkey") {
		return ErrEventKindInUse
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEventKindNotFound
	}
	return nil
}

func (r *EventRepository) KindByID(ctx context.Context, id int64) (*models.EventKind, error) {
	var k models.EventKind
	err := r.pool.QueryRow(ctx, `SELECT id, organization_id, title, position FROM event_kinds WHERE id = $1`, id).
		Scan(&k.ID, &k.OrganizationID, &k.Title, &k.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventKindNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *EventRepository) CreateEvent(ctx context.Context, e *models.Event) error {
	var created, updated any
	cols := []string{"kind_id", "slug", "title", "dates", "place", "description", "position"}
	args := []any{e.KindID, e.Slug, e.Title, e.Dates, e.Place, e.Description, e.Position}
	err := insertRow(ctx, r.pool, "events", e.ID, cols, args, &e.ID, &created, &updated)
	if isUnique(err, "events_slug_key") {
		return fmt.Errorf("%w: %s", ErrEventSlugTaken, e.Slug)
	}
	if isFKViolation(err, "events_kind_id_fkey") {
		return ErrEventKindNotFound
	}
	return eventIDErr(err)
}

// UpdateEvent слаг не пишет; вид (переезд в другой) пишет.
func (r *EventRepository) UpdateEvent(ctx context.Context, e *models.Event) error {
	tag, err := r.pool.Exec(ctx, `UPDATE events
		SET kind_id = $2, title = $3, dates = $4, place = $5, description = $6, position = $7, updated_at = NOW()
		WHERE id = $1`, e.ID, e.KindID, e.Title, e.Dates, e.Place, e.Description, e.Position)
	if isFKViolation(err, "events_kind_id_fkey") {
		return ErrEventKindNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEventNotFound
	}
	return nil
}

func (r *EventRepository) DeleteEvent(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM events WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEventNotFound
	}
	return nil
}

func (r *EventRepository) EventByID(ctx context.Context, id int64) (*models.Event, error) {
	var e models.Event
	err := r.pool.QueryRow(ctx, `SELECT id, kind_id, slug, title, dates, place, description, position
		FROM events WHERE id = $1`, id).
		Scan(&e.ID, &e.KindID, &e.Slug, &e.Title, &e.Dates, &e.Place, &e.Description, &e.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// Organizations — все организации с видами и событиями.
func (r *EventRepository) Organizations(ctx context.Context) ([]models.EventOrganizationDetail, error) {
	return r.organizations(ctx, "")
}

func (r *EventRepository) OrganizationDetail(ctx context.Context, slug string) (*models.EventOrganizationDetail, error) {
	orgs, err := r.organizations(ctx, slug)
	if err != nil {
		return nil, err
	}
	if len(orgs) == 0 {
		return nil, ErrEventOrgNotFound
	}
	return &orgs[0], nil
}

func (r *EventRepository) organizations(ctx context.Context, slug string) ([]models.EventOrganizationDetail, error) {
	orows, err := r.pool.Query(ctx, `SELECT id, slug, title, description, position FROM event_organizations
		WHERE $1 = '' OR slug = $1 ORDER BY position, title, id`, slug)
	if err != nil {
		return nil, err
	}
	out := []models.EventOrganizationDetail{}
	orgIdx := map[int64]int{}
	for orows.Next() {
		var o models.EventOrganizationDetail
		if err := orows.Scan(&o.ID, &o.Slug, &o.Title, &o.Description, &o.Position); err != nil {
			orows.Close()
			return nil, err
		}
		o.Kinds = []models.EventKindEvents{}
		orgIdx[o.ID] = len(out)
		out = append(out, o)
	}
	orows.Close()
	if err := orows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	krows, err := r.pool.Query(ctx, `SELECT k.id, k.organization_id, k.title, k.position
		FROM event_kinds k JOIN event_organizations o ON o.id = k.organization_id
		WHERE $1 = '' OR o.slug = $1 ORDER BY k.position, k.id`, slug)
	if err != nil {
		return nil, err
	}
	type kpos struct{ org, kind int }
	kindIdx := map[int64]kpos{}
	for krows.Next() {
		var k models.EventKindEvents
		if err := krows.Scan(&k.ID, &k.OrganizationID, &k.Title, &k.Position); err != nil {
			krows.Close()
			return nil, err
		}
		k.Events = []models.EventSummary{}
		oi := orgIdx[k.OrganizationID]
		kindIdx[k.ID] = kpos{oi, len(out[oi].Kinds)}
		out[oi].Kinds = append(out[oi].Kinds, k)
	}
	krows.Close()
	if err := krows.Err(); err != nil {
		return nil, err
	}

	erows, err := r.pool.Query(ctx, `
		SELECT e.id, e.kind_id, e.slug, e.title, e.dates,
		       COALESCE(array_agg(t.work_id) FILTER (WHERE t.work_id IS NOT NULL
		           AND (t.kind = 'work' OR t.chapter_id IS NOT NULL)), '{}')
		FROM events e
		JOIN event_kinds k ON k.id = e.kind_id
		JOIN event_organizations o ON o.id = k.organization_id
		LEFT JOIN event_sections s ON s.event_id = e.id
		LEFT JOIN event_entries en ON en.section_id = s.id
		LEFT JOIN event_targets t ON t.entry_id = en.id
		WHERE $1 = '' OR o.slug = $1
		GROUP BY e.id ORDER BY e.position, e.id`, slug)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var s models.EventSummary
		var kindID int64
		if err := erows.Scan(&s.ID, &kindID, &s.Slug, &s.Title, &s.Dates, &s.TargetWorkIDs); err != nil {
			return nil, err
		}
		s.MaterialsTotal = len(s.TargetWorkIDs)
		p := kindIdx[kindID]
		out[p.org].Kinds[p.kind].Events = append(out[p.org].Kinds[p.kind].Events, s)
	}
	return out, erows.Err()
}

// EventDetail — событие по слагу с повесткой.
func (r *EventRepository) EventDetail(ctx context.Context, slug string) (*models.EventDetail, error) {
	var d models.EventDetail
	err := r.pool.QueryRow(ctx, `
		SELECT e.id, e.kind_id, e.slug, e.title, e.dates, e.place, e.description, e.position,
		       k.title, o.id, o.slug, o.title, o.description, o.position
		FROM events e
		JOIN event_kinds k ON k.id = e.kind_id
		JOIN event_organizations o ON o.id = k.organization_id
		WHERE e.slug = $1`, slug).
		Scan(&d.ID, &d.KindID, &d.Slug, &d.Title, &d.Dates, &d.Place, &d.Description, &d.Position,
			&d.KindTitle, &d.Organization.ID, &d.Organization.Slug, &d.Organization.Title,
			&d.Organization.Description, &d.Organization.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Sections = []models.EventSection{}

	rows, err := r.pool.Query(ctx, `
		SELECT s.position, s.title, en.position, en.label,
		       t.kind, t.work_id, t.chapter_id, t.label,
		       CASE WHEN t.kind = 'chapter' THEN COALESCE(c.title, t.snapshot_title)
		            ELSE COALESCE(w.title, t.snapshot_title) END,
		       COALESCE(w.title, ''),
		       (t.work_id IS NULL OR (t.kind = 'chapter' AND t.chapter_id IS NULL))
		FROM event_sections s
		LEFT JOIN event_entries en ON en.section_id = s.id
		LEFT JOIN event_targets t ON t.entry_id = en.id
		LEFT JOIN works w ON w.id = t.work_id
		LEFT JOIN chapters c ON c.id = t.chapter_id
		WHERE s.event_id = $1
		ORDER BY s.position, en.position, t.position`, d.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lastEntry := -1 // позиция последней строки текущего пункта
	for rows.Next() {
		var (
			sPos              int
			sTitle            string
			ePos              *int
			eLabel            *string
			tKind, tLabel     *string
			tTitle, workTitle *string
			workID, chapterID *int64
			broken            *bool
		)
		if err := rows.Scan(&sPos, &sTitle, &ePos, &eLabel, &tKind, &workID, &chapterID, &tLabel,
			&tTitle, &workTitle, &broken); err != nil {
			return nil, err
		}
		if n := len(d.Sections); n == 0 || d.Sections[n-1].Position != sPos {
			d.Sections = append(d.Sections, models.EventSection{Position: sPos, Title: sTitle, Entries: []models.EventEntry{}})
			lastEntry = -1
		}
		sec := &d.Sections[len(d.Sections)-1]
		if ePos == nil {
			continue
		}
		if *ePos != lastEntry {
			sec.Entries = append(sec.Entries, models.EventEntry{Label: *eLabel, Targets: []models.EventTarget{}})
			lastEntry = *ePos
		}
		if tKind == nil {
			continue
		}
		ent := &sec.Entries[len(sec.Entries)-1]
		ent.Targets = append(ent.Targets, models.EventTarget{
			Kind: *tKind, WorkID: workID, ChapterID: chapterID, Label: *tLabel,
			Title: *tTitle, WorkTitle: *workTitle, Broken: *broken,
		})
	}
	return &d, rows.Err()
}

// ReplaceAgenda заменяет повестку события одной транзакцией. Заглавие-снимок
// цели берётся из живой главы или работы здесь же. Глава не из work_id,
// несуществующая работа или глава — ErrAgendaTarget; повестка при отказе
// остаётся прежней.
func (r *EventRepository) ReplaceAgenda(ctx context.Context, eventID int64, in models.AgendaInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // после Commit это no-op
	var lock int64
	if err := tx.QueryRow(ctx, `SELECT id FROM events WHERE id = $1 FOR UPDATE`, eventID).Scan(&lock); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM event_sections WHERE event_id = $1`, eventID); err != nil {
		return err
	}
	for si, sec := range in.Sections {
		var sectionID int64
		if err := tx.QueryRow(ctx, `INSERT INTO event_sections (event_id, position, title)
			VALUES ($1, $2, $3) RETURNING id`, eventID, si, sec.Title).Scan(&sectionID); err != nil {
			return err
		}
		for ei, ent := range sec.Entries {
			var entryID int64
			if err := tx.QueryRow(ctx, `INSERT INTO event_entries (section_id, position, label)
				VALUES ($1, $2, $3) RETURNING id`, sectionID, ei, ent.Label).Scan(&entryID); err != nil {
				return err
			}
			for ti, tg := range ent.Targets {
				where := fmt.Sprintf("пункт %d, строка %d, цель %d", si+1, ei+1, ti+1)
				title, chapterID, reason, err := agendaTargetTitle(ctx, tx, tg)
				if err != nil {
					return err
				}
				if reason != "" {
					return fmt.Errorf("%w: %s: %s", ErrAgendaTarget, where, reason)
				}
				if _, err := tx.Exec(ctx, `INSERT INTO event_targets
					(entry_id, position, kind, work_id, chapter_id, label, snapshot_title)
					VALUES ($1, $2, $3, $4, $5, $6, $7)`,
					entryID, ti, tg.Kind, tg.WorkID, chapterID, tg.Label, title); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit(ctx)
}

// agendaTargetTitle проверяет цель и отдаёт её живое заглавие. Непустая
// reason — цель недопустима (ErrAgendaTarget); err — сбой базы.
func agendaTargetTitle(ctx context.Context, tx pgx.Tx, tg models.AgendaTargetInput) (title string, chapterID *int64, reason string, err error) {
	switch tg.Kind {
	case models.EventTargetWork:
		if tg.ChapterID != nil {
			return "", nil, "у цели-работы chapter_id не бывает", nil
		}
		err = tx.QueryRow(ctx, `SELECT title FROM works WHERE id = $1`, tg.WorkID).Scan(&title)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, "работы нет", nil
		}
		return title, nil, "", err
	case models.EventTargetChapter:
		if tg.ChapterID == nil {
			return "", nil, "у цели-главы нужен chapter_id", nil
		}
		err = tx.QueryRow(ctx, `SELECT title FROM chapters WHERE id = $1 AND work_id = $2`,
			*tg.ChapterID, tg.WorkID).Scan(&title)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, "главы нет в этой работе", nil
		}
		return title, tg.ChapterID, "", err
	}
	return "", nil, "вид цели — chapter или work", nil
}

// ChapterEvents — события, в повестке которых стоит глава («Входит в: …»).
// Глава не из workID или несуществующая — ErrChapterNotInWork.
func (r *EventRepository) ChapterEvents(ctx context.Context, workID, chapterID int64) ([]models.ChapterEventLink, error) {
	var one int
	err := r.pool.QueryRow(ctx, `SELECT 1 FROM chapters WHERE id = $1 AND work_id = $2`, chapterID, workID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChapterNotInWork
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT o.slug, o.title, e.slug, e.title, s.title, s.position
		FROM event_targets t
		JOIN event_entries en ON en.id = t.entry_id
		JOIN event_sections s ON s.id = en.section_id
		JOIN events e ON e.id = s.event_id
		JOIN event_kinds k ON k.id = e.kind_id
		JOIN event_organizations o ON o.id = k.organization_id
		WHERE t.chapter_id = $1
		GROUP BY o.id, e.id, s.id
		ORDER BY o.position, o.title, e.position, e.id, s.position`, chapterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ChapterEventLink{}
	for rows.Next() {
		var l models.ChapterEventLink
		if err := rows.Scan(&l.OrganizationSlug, &l.OrganizationTitle, &l.EventSlug, &l.EventTitle,
			&l.SectionTitle, &l.SectionPosition); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
