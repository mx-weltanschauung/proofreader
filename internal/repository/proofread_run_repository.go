package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"proofreader/internal/models"
	"proofreader/internal/proofassist"
)

var (
	ErrProofreadRunNotFound     = errors.New("proofread run or target not found")
	ErrProofreadBaseMismatch    = errors.New("proofread page snapshot hash mismatch")
	ErrProofreadInvalidFinding  = errors.New("invalid proofread finding")
	ErrProofreadInvalidDecision = errors.New("invalid proofread finding decision")
	ErrProofreadPageSubmitted   = errors.New("proofread page already submitted")
	ErrProofreadCredential      = errors.New("invalid or expired proofread credential")
)

const (
	proofreadPairCodeLifetime = 10 * time.Minute
	proofreadTokenLifetime    = 24 * time.Hour
	proofreadRunColumns       = "id, user_id, work_id, chapter_id, created_at"
)

type ProofreadRunRepository struct {
	pool *pgxpool.Pool
}

func NewProofreadRunRepository(pool *pgxpool.Pool) *ProofreadRunRepository {
	return &ProofreadRunRepository{pool: pool}
}

func scanProofreadRun(row pgx.Row) (models.ProofreadRun, error) {
	var run models.ProofreadRun
	err := row.Scan(&run.ID, &run.UserID, &run.WorkID, &run.ChapterID, &run.CreatedAt)
	return run, err
}

// Create snapshots the pages in the chapter's inclusive range, including pages
// assigned to a nested chapter. The caller resolves reader access separately.
func (r *ProofreadRunRepository) Create(ctx context.Context, userID, workID, chapterID int64) (models.ProofreadRun, error) {
	run, err := scanProofreadRun(r.pool.QueryRow(ctx, `
		WITH new_run AS (
			INSERT INTO proofread_runs (user_id, work_id, chapter_id)
			SELECT u.id, c.work_id, c.id
			FROM chapters c
			JOIN users u ON u.id = $1 AND u.role = 'reader'
			WHERE c.work_id = $2 AND c.id = $3
			RETURNING `+proofreadRunColumns+`
		), snapshots AS (
			INSERT INTO proofread_run_pages (run_id, page_id, page_number, base_markdown, base_sha256)
			SELECT r.id, p.id, p.page_number, COALESCE(p.content_markdown, ''),
			       encode(sha256(convert_to(COALESCE(p.content_markdown, ''), 'UTF8')), 'hex')
			FROM new_run r
			JOIN chapters c ON c.id = r.chapter_id
			JOIN pages p ON p.work_id = r.work_id AND p.page_number BETWEEN c.start_page AND c.end_page
		)
		SELECT `+proofreadRunColumns+` FROM new_run`, userID, workID, chapterID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run, ErrProofreadRunNotFound
	}
	return run, err
}

func (r *ProofreadRunRepository) LatestForUser(ctx context.Context, userID, workID, chapterID int64) (models.ProofreadRun, error) {
	run, err := scanProofreadRun(r.pool.QueryRow(ctx, `SELECT `+proofreadRunColumns+` FROM proofread_runs WHERE user_id=$1 AND work_id=$2 AND chapter_id=$3 ORDER BY created_at DESC,id DESC LIMIT 1`, userID, workID, chapterID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run, ErrProofreadRunNotFound
	}
	return run, err
}

func (r *ProofreadRunRepository) GetForUser(ctx context.Context, runID, userID int64) (models.ProofreadRun, error) {
	run, err := scanProofreadRun(r.pool.QueryRow(ctx, `
		SELECT `+proofreadRunColumns+` FROM proofread_runs WHERE id = $1 AND user_id = $2`, runID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return run, ErrProofreadRunNotFound
	}
	return run, err
}

// ListPages is an internal read. The caller verifies run ownership or worker
// credentials and applies current corpus and scan access rules before disclosure.
func (r *ProofreadRunRepository) ListPages(ctx context.Context, runID int64) ([]models.ProofreadRunPage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.page_id, p.page_number, p.base_markdown, p.base_sha256, p.analysis_status,
		       p.analysis_error, p.suggestion_id, p.submitted_at,
		       COALESCE((
		           SELECT jsonb_agg(to_jsonb(f) ORDER BY f.start_rune, f.end_rune, f.id)
		           FROM proofread_findings f WHERE f.run_id = p.run_id AND f.page_id = p.page_id
		       ), '[]'::jsonb)
		FROM proofread_run_pages p
		WHERE p.run_id = $1
		ORDER BY p.page_number, p.page_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pages := make([]models.ProofreadRunPage, 0)
	for rows.Next() {
		var p models.ProofreadRunPage
		if err := rows.Scan(&p.PageID, &p.PageNumber, &p.BaseMarkdown, &p.BaseSHA256, &p.AnalysisStatus,
			&p.AnalysisError, &p.SuggestionID, &p.SubmittedAt, &p.Findings); err != nil {
			return nil, err
		}
		for i := range p.Findings {
			if string(p.Findings[i].ScanBBox) == "null" {
				p.Findings[i].ScanBBox = nil
			}
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

// lockProofreadRunPage serializes uploads, decisions and submission for the
// same page snapshot. Submission must acquire the same row lock.
func lockProofreadRunPage(ctx context.Context, tx pgx.Tx, runID, pageID int64, baseSHA256 string) (models.ProofreadRunPage, error) {
	var page models.ProofreadRunPage
	err := tx.QueryRow(ctx, `
		SELECT base_markdown, base_sha256, analysis_status, submitted_at
		FROM proofread_run_pages WHERE run_id = $1 AND page_id = $2 FOR UPDATE`, runID, pageID).
		Scan(&page.BaseMarkdown, &page.BaseSHA256, &page.AnalysisStatus, &page.SubmittedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return page, ErrProofreadRunNotFound
	}
	if err == nil && page.BaseSHA256 != baseSHA256 {
		return page, ErrProofreadBaseMismatch
	}
	return page, err
}

func (r *ProofreadRunRepository) PutFindings(ctx context.Context, runID, pageID int64, baseSHA256 string, findings []models.ProofreadFinding) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		page, err := lockProofreadRunPage(ctx, tx, runID, pageID, baseSHA256)
		if err != nil {
			return err
		}
		base := []rune(page.BaseMarkdown)
		for i, finding := range findings {
			if strings.TrimSpace(finding.CandidateKey) == "" || utf8.RuneCountInString(finding.CandidateKey) > 256 ||
				finding.StartRune < 0 || finding.EndRune < finding.StartRune || finding.EndRune > len(base) ||
				string(base[finding.StartRune:finding.EndRune]) != finding.Old {
				return fmt.Errorf("%w at index %d", ErrProofreadInvalidFinding, i)
			}
		}
		if page.SubmittedAt != nil {
			return nil
		}
		for _, finding := range findings {
			_, err := tx.Exec(ctx, `
				INSERT INTO proofread_findings (run_id, page_id, candidate_key, start_rune, end_rune,
				                              old, new, source, reason, scan_bbox)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (run_id, page_id, candidate_key) DO NOTHING`, runID, pageID,
				finding.CandidateKey, finding.StartRune, finding.EndRune, finding.Old,
				finding.New, finding.Source, finding.Reason, finding.ScanBBox)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `
			UPDATE proofread_run_pages SET analysis_status = 'analyzed', analysis_error = ''
			WHERE run_id = $1 AND page_id = $2`, runID, pageID)
		return err
	})
}

// SetPageAnalysisError records a retryable failure. A late failure report does
// not overwrite a successful upload.
func (r *ProofreadRunRepository) SetPageAnalysisError(ctx context.Context, runID, pageID int64, baseSHA256, message string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		page, err := lockProofreadRunPage(ctx, tx, runID, pageID, baseSHA256)
		if err != nil {
			return err
		}
		if page.AnalysisStatus == models.ProofreadAnalysisAnalyzed || page.SubmittedAt != nil {
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE proofread_run_pages SET analysis_status = 'failed', analysis_error = $3
			WHERE run_id = $1 AND page_id = $2`, runID, pageID, message)
		return err
	})
}

// SetDecision takes the accepted replacement verbatim. An empty replacement
// for accepted means deletion; callers supply the proposed text for a plain accept.
func (r *ProofreadRunRepository) SetDecision(ctx context.Context, runID, findingID, userID int64, decision models.FindingDecision, replacement string) error {
	switch decision {
	case models.FindingPending, models.FindingRejected:
		replacement = ""
	case models.FindingAccepted:
	default:
		return ErrProofreadInvalidDecision
	}

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var submittedAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT p.submitted_at FROM proofread_runs r
			JOIN proofread_findings f ON f.run_id = r.id
			JOIN proofread_run_pages p ON p.run_id = f.run_id AND p.page_id = f.page_id
			WHERE r.id = $1 AND f.id = $2 AND r.user_id = $3
			FOR UPDATE OF p`, runID, findingID, userID).Scan(&submittedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProofreadRunNotFound
		}
		if err != nil {
			return err
		}
		if submittedAt != nil {
			return ErrProofreadPageSubmitted
		}
		_, err = tx.Exec(ctx, `
			UPDATE proofread_findings SET decision = $3, replacement = $4
			WHERE run_id = $1 AND id = $2`, runID, findingID, decision, replacement)
		return err
	})
}

func (r *ProofreadRunRepository) IssuePairCode(ctx context.Context, runID, userID int64) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	code := hex.EncodeToString(random[:])
	hash := sha256.Sum256([]byte(code))
	tag, err := r.pool.Exec(ctx, `
		UPDATE proofread_runs SET pair_code_hash = $3,
		       pair_code_expires_at = clock_timestamp() + make_interval(secs => $4)
		WHERE id = $1 AND user_id = $2`, runID, userID, hash[:], proofreadPairCodeLifetime.Seconds())
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrProofreadRunNotFound
	}
	return code, nil
}

func (r *ProofreadRunRepository) ConsumePairCode(ctx context.Context, code string) (models.ProofreadRun, string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return models.ProofreadRun{}, "", err
	}
	token := hex.EncodeToString(random[:])
	codeHash := sha256.Sum256([]byte(code))
	tokenHash := sha256.Sum256([]byte(token))
	run, err := scanProofreadRun(r.pool.QueryRow(ctx, `
		UPDATE proofread_runs SET pair_code_hash = NULL, pair_code_expires_at = NULL,
		       worker_token_hash = $2,
		       worker_token_expires_at = clock_timestamp() + make_interval(secs => $3)
		WHERE pair_code_hash = $1 AND pair_code_expires_at > clock_timestamp()
		RETURNING `+proofreadRunColumns, codeHash[:], tokenHash[:], proofreadTokenLifetime.Seconds()))
	if errors.Is(err, pgx.ErrNoRows) {
		return run, "", ErrProofreadCredential
	}
	if err != nil {
		return run, "", err
	}
	return run, token, nil
}

func (r *ProofreadRunRepository) GetForWorker(ctx context.Context, runID int64, token string) (models.ProofreadRun, error) {
	hash := sha256.Sum256([]byte(token))
	run, err := scanProofreadRun(r.pool.QueryRow(ctx, `
		SELECT `+proofreadRunColumns+` FROM proofread_runs
		WHERE id = $1 AND worker_token_hash = $2 AND worker_token_expires_at > clock_timestamp()`, runID, hash[:]))
	if errors.Is(err, pgx.ErrNoRows) {
		return run, ErrProofreadCredential
	}
	return run, err
}

// Для проверки доступа достаточно снимков полос. Решения читаются внутри
// SubmitReadyPage после блокировки снимка.
func (r *ProofreadRunRepository) ListSubmissionPages(ctx context.Context, runID int64) ([]models.ProofreadRunPage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT page_id, page_number, base_markdown, submitted_at, suggestion_id
		FROM proofread_run_pages WHERE run_id = $1 ORDER BY page_number, page_id`, runID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.ProofreadRunPage, error) {
		var p models.ProofreadRunPage
		err := row.Scan(&p.PageID, &p.PageNumber, &p.BaseMarkdown, &p.SubmittedAt, &p.SuggestionID)
		return p, err
	})
}

func (r *ProofreadRunRepository) SubmitReadyPage(ctx context.Context, runID, userID, pageID int64, ipHash string, limit int, since time.Time) (models.PageSubmitResult, error) {
	result := models.PageSubmitResult{PageID: pageID, Status: "failed"}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var page models.ProofreadRunPage
		var liveHash string
		err := tx.QueryRow(ctx, `
			SELECT rp.base_markdown, rp.base_sha256, rp.submitted_at, rp.suggestion_id,
			       encode(sha256(convert_to(COALESCE(p.content_markdown, ''), 'UTF8')), 'hex')
			FROM proofread_run_pages rp
			JOIN proofread_runs r ON r.id = rp.run_id
			JOIN users u ON u.id = r.user_id AND u.role = 'reader'
			JOIN pages p ON p.id = rp.page_id
			WHERE rp.run_id = $1 AND r.user_id = $2 AND rp.page_id = $3
			FOR UPDATE OF rp FOR SHARE OF p`, runID, userID, pageID).Scan(&page.BaseMarkdown, &page.BaseSHA256, &page.SubmittedAt, &page.SuggestionID, &liveHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProofreadRunNotFound
		}
		if err != nil {
			return err
		}
		if page.SubmittedAt != nil {
			result.Status = "already_submitted"
			result.SuggestionID = page.SuggestionID
			return nil
		}
		if page.BaseSHA256 != liveHash {
			result.Status = "stale"
			return nil
		}
		rows, err := tx.Query(ctx, `
			SELECT decision, start_rune, end_rune, old, replacement
			FROM proofread_findings WHERE run_id = $1 AND page_id = $2`, runID, pageID)
		if err != nil {
			return err
		}
		defer rows.Close()
		var edits []proofassist.Edit
		for rows.Next() {
			var d models.FindingDecision
			var e proofassist.Edit
			if err := rows.Scan(&d, &e.StartRune, &e.EndRune, &e.Old, &e.New); err != nil {
				return err
			}
			if d == models.FindingPending {
				result.Status = "pending"
				return nil
			}
			if d == models.FindingAccepted {
				edits = append(edits, e)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(edits) == 0 {
			result.Status = "pending"
			return nil
		}
		proposed, err := proofassist.Apply(page.BaseMarkdown, edits)
		if err != nil {
			return nil
		}
		const note = "Указанные фрагменты сверены со сканом."
		if models.ValidateSuggestion(page.BaseMarkdown, proposed, note) != nil {
			return nil
		}
		s := models.PageSuggestion{PageID: pageID, UserID: &userID, BaseMarkdown: page.BaseMarkdown, ProposedMarkdown: proposed, Note: note, IPHash: ipHash}
		accepted, err := createSuggestionWithinLimit(ctx, tx, &s, limit, since)
		if err != nil {
			return err
		}
		if !accepted {
			result.Status = "rate_limited"
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE proofread_run_pages SET submitted_at = clock_timestamp(), suggestion_id = $3
			WHERE run_id = $1 AND page_id = $2`, runID, pageID, s.ID)
		if err != nil {
			return err
		}
		result.Status = "submitted"
		result.SuggestionID = &s.ID
		return nil
	})
	if err != nil {
		return models.PageSubmitResult{PageID: pageID, Status: "failed"}, err
	}
	return result, nil
}
