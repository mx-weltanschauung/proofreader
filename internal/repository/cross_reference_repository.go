package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
)

type CrossReferenceRepository struct {
	pool *pgxpool.Pool
}

func NewCrossReferenceRepository(pool *pgxpool.Pool) *CrossReferenceRepository {
	return &CrossReferenceRepository{pool: pool}
}

// InScope restricts a scan to a numbered volume of the selected edition.
func (r *CrossReferenceRepository) InScope(ctx context.Context, editionID, workID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM editions e JOIN works sw ON sw.edition_id = e.id
		WHERE e.id = $1 AND sw.id = $2 AND sw.role = 'volume'
		  AND sw.numbering_style = 'arabic' AND sw.volume_number IS NOT NULL AND e.volume_numbering = 'printed')
	`, editionID, workID).Scan(&exists)
	return exists, err
}

// SHA-256 binds a decision to one revision of the source text. A stale
// snapshot, including one from a concurrent scan, cannot replace a decision.
func (r *CrossReferenceRepository) Add(ctx context.Context, editionID, workID int64, found []models.CrossReferenceOccurrence) (int, error) {
	if len(found) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, ref := range found {
		batch.Queue(`
			INSERT INTO cross_references (edition_id, edition_designation, source_page_id, source_digest,
				note, ordinal, quote, context, volume, printed_page, edition_number)
			SELECT e.id, `+editionDesignation+`, sp.id, $4, $5, $6, $7, $8, $9, $10, $11
			FROM pages sp JOIN works sw ON sw.id = sp.work_id
			JOIN editions e ON e.id = sw.edition_id
			WHERE e.id = $1 AND sw.id = $2 AND sp.id = $3
			  AND sw.role = 'volume' AND sw.numbering_style = 'arabic' AND sw.volume_number IS NOT NULL AND e.volume_numbering = 'printed'
			  AND sha256(convert_to(sp.content_markdown, 'UTF8')) = $4
			ON CONFLICT (source_page_id, edition_id, edition_designation, source_digest, note, ordinal) DO NOTHING
		`, editionID, workID, ref.SourcePageID, ref.SourceDigest, ref.Note, ref.Ordinal,
			ref.Quote, ref.Context, ref.Volume, ref.PrintedPage, ref.EditionNumber)
	}
	results := r.pool.SendBatch(ctx, batch)
	var added int
	for range found {
		tag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return 0, err
		}
		added += int(tag.RowsAffected())
	}
	return added, results.Close()
}

// The edition ID is the identity. A title may also carry a bibliographic
// designation such as «5-е изд.»; only one unambiguous number is retained.
const editionDesignation = `coalesce((
	SELECT min(m[1]::integer)
	FROM regexp_matches(e.title, '(?:^|[^[:alnum:]])([0-9]{1,9})(?:[-‐‑–]?е)?[[:space:]]+изд(?:ание|ания|\.)', 'gi') m
	HAVING count(*) = 1
), 0)`

const referenceEditionMatches = `(r.edition_number = 0 OR r.edition_number = r.edition_designation)`

// Predicates shared by review, publication, and the redirect. The printed
// folio is the ordinal plus the work's offset (as in the subject index).
// Check the actual page row: a volume's extent does not prove it has a folio.
const currentReferenceSource = `
	r.edition_designation = ` + editionDesignation + ` AND e.volume_numbering = 'printed'
	AND sw.edition_id = r.edition_id AND sw.role = 'volume' AND sw.numbering_style = 'arabic' AND sw.volume_number IS NOT NULL
	AND sha256(convert_to(sp.content_markdown, 'UTF8')) = r.source_digest
`

const referenceAddress = `
	tw.edition_id = r.edition_id AND tw.role = 'volume'
	AND tw.volume_number = r.volume AND tw.numbering_style = 'arabic'
	AND tp.page_number = r.printed_page::bigint - tw.page_offset
`

const closedReferenceSource = `EXISTS (
	SELECT 1 FROM closed_pages cp WHERE cp.work_id = sp.work_id AND cp.page_number = sp.page_number
)`
const closedReferenceTarget = `EXISTS (
	SELECT 1 FROM closed_pages cp WHERE cp.work_id = tp.work_id AND cp.page_number = tp.page_number
)`

const listCrossReferences = `
	SELECT r.id, r.quote, r.context, r.volume, r.printed_page, r.edition_number,
	       r.status, r.edition_designation, r.target_page_id,
	       sp.work_id, sp.id, sp.page_number, sp.page_number::bigint + sw.page_offset,
	       sw.title, coalesce(sw.volume_part, ''), ` + closedReferenceSource + `,
	       coalesce((` + currentReferenceSource + `), false) AS current_source,
	       coalesce(options.targets, '[]'::jsonb)
	FROM cross_references r
	JOIN pages sp ON sp.id = r.source_page_id
	JOIN works sw ON sw.id = sp.work_id
	JOIN editions e ON e.id = r.edition_id
	LEFT JOIN LATERAL (
		SELECT jsonb_agg(jsonb_build_object(
			'work_id', tw.id, 'page_id', tp.id, 'page_number', tp.page_number,
			'printed_page', r.printed_page, 'work_title', tw.title,
			'volume_part', tw.volume_part, 'closed', ` + closedReferenceTarget + `
		) ORDER BY tw.volume_part NULLS FIRST, tw.id) AS targets
		FROM works tw JOIN pages tp ON tp.work_id = tw.id
		WHERE ` + referenceAddress + `
		  AND ` + referenceEditionMatches + ` AND (` + currentReferenceSource + `)
	) options ON true
	WHERE r.edition_id = $1 AND sp.work_id = $2 AND r.id > $3 AND r.status = $4
	  AND ($4 <> 'pending' OR (` + currentReferenceSource + `))
	ORDER BY r.id LIMIT $5
`

func (r *CrossReferenceRepository) List(ctx context.Context, editionID, workID, after int64, status string, limit int) ([]models.CrossReferenceCandidate, error) {
	rows, err := r.pool.Query(ctx, listCrossReferences, editionID, workID, after, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CrossReferenceCandidate, 0)
	for rows.Next() {
		var c models.CrossReferenceCandidate
		var current bool
		var designation int
		err = rows.Scan(&c.ID, &c.Quote, &c.Context, &c.Volume, &c.PrintedPage, &c.EditionNumber,
			&c.Status, &designation, &c.TargetPageID,
			&c.Source.WorkID, &c.Source.PageID, &c.Source.PageNumber, &c.Source.PrintedPage,
			&c.Source.WorkTitle, &c.Source.VolumePart, &c.Source.Closed, &current, &c.Targets)
		if err != nil {
			return nil, err
		}
		switch {
		case !current || (c.Status == models.CrossReferenceConfirmed && !hasReferenceTarget(c.Targets, c.TargetPageID)):
			c.Issue = "stale"
			c.Targets = nil
		case c.EditionNumber > 0 && designation == 0:
			c.Issue = "edition_uncertain"
		case c.EditionNumber > 0 && c.EditionNumber != designation:
			c.Issue = "edition_mismatch"
		case len(c.Targets) == 0:
			c.Issue = "missing"
		case len(c.Targets) > 1 && c.Status == models.CrossReferencePending:
			c.Issue = "ambiguous"
		}
		if c.Targets == nil {
			c.Targets = []models.CrossReferenceLocation{}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func hasReferenceTarget(targets []models.CrossReferenceLocation, id *int64) bool {
	if id == nil {
		return false
	}
	for _, target := range targets {
		if target.PageID == *id {
			return true
		}
	}
	return false
}

func (r *CrossReferenceRepository) Decide(ctx context.Context, editionID, workID, id int64, status string, target *int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE cross_references r SET status = $4, target_page_id = $5::bigint
		FROM pages sp JOIN works sw ON sw.id = sp.work_id, editions e
		WHERE r.edition_id = $1 AND sw.id = $2 AND r.id = $3 AND r.status = 'pending'
		  AND sp.id = r.source_page_id AND e.id = r.edition_id AND (`+currentReferenceSource+`)
		  AND (($4 = 'rejected' AND $5::bigint IS NULL) OR ($4 = 'confirmed' AND `+referenceEditionMatches+` AND EXISTS (
			SELECT 1 FROM works tw JOIN pages tp ON tp.work_id = tw.id
			WHERE tp.id = $5 AND `+referenceAddress+`
		  )))
	`, editionID, workID, id, status, target)
	return tag.RowsAffected() == 1, err
}

const publishedCrossReferences = `
	SELECT r.id, r.source_page_id, r.source_digest, r.note, r.ordinal, r.quote,
	       tp.id, tp.work_id, tp.page_number
	FROM cross_references r
	JOIN editions e ON e.id = r.edition_id
	JOIN pages sp ON sp.id = r.source_page_id
	JOIN works sw ON sw.id = sp.work_id
	JOIN pages tp ON tp.id = r.target_page_id
	JOIN works tw ON tw.id = tp.work_id
	WHERE r.status = 'confirmed' AND ` + referenceEditionMatches + `
	  AND (` + currentReferenceSource + `) AND (` + referenceAddress + `)
	  AND NOT ` + closedReferenceSource + ` AND NOT ` + closedReferenceTarget + `
`

func (r *CrossReferenceRepository) Published(ctx context.Context, editionID, workID int64, start, end int) ([]models.PublishedCrossReference, error) {
	rows, err := r.pool.Query(ctx, publishedCrossReferences+` AND r.edition_id = $1 AND sp.work_id = $2 AND sp.page_number BETWEEN $3 AND $4`, editionID, workID, start, end)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[models.PublishedCrossReference])
}

func (r *CrossReferenceRepository) Destination(ctx context.Context, editionID, id int64) (models.PublishedCrossReference, bool, error) {
	rows, err := r.pool.Query(ctx, publishedCrossReferences+` AND r.edition_id = $1 AND r.id = $2`, editionID, id)
	if err != nil {
		return models.PublishedCrossReference{}, false, err
	}
	ref, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[models.PublishedCrossReference])
	if errors.Is(err, pgx.ErrNoRows) {
		return ref, false, nil
	}
	return ref, err == nil, err
}
