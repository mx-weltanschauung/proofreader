package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"proofreader/internal/models"
)

const proofreadBase = "кот кот"

func proofreadFixture(t *testing.T) (context.Context, *ProofreadRunRepository, models.ProofreadRun, int64, int64) {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)
	users := NewUserRepository(pool)
	owner := createReader(t, users, "Корректор", "proofread-owner")
	other := createReader(t, users, "Другой читатель", "proofread-other")
	_, err := pool.Exec(ctx, `
		INSERT INTO works (id, title, file_path, owner_id) VALUES (10, 'Том', '/book', 1), (11, 'Другой том', '/other', 1);
		INSERT INTO chapters (id, work_id, title, type, order_number, start_page, end_page)
		VALUES (20, 10, 'Глава', 'chapter', 1, 5, 6);
		INSERT INTO pages (id, work_id, page_number, content_markdown)
		VALUES (30, 10, 5, 'кот кот'), (31, 10, 6, ''), (32, 10, 7, 'за главой'), (33, 11, 5, 'другой том');
	`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewProofreadRunRepository(pool)
	run, err := repo.Create(ctx, owner.ID, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, repo, run, owner.ID, other.ID
}

func proofreadFindings(t *testing.T, ctx context.Context, repo *ProofreadRunRepository, runID int64) models.ProofreadRunPage {
	t.Helper()
	pages, err := repo.ListPages(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || pages[0].PageID != 30 || pages[0].PageNumber != 5 || pages[1].PageID != 31 {
		t.Fatalf("wrong chapter pages: %+v", pages)
	}
	return pages[0]
}

func proofreadCandidate() models.ProofreadFinding {
	return models.ProofreadFinding{
		CandidateKey: "second-cat", StartRune: 4, EndRune: 7, Old: "кот",
		New: "кит", Source: "model", Reason: "по скану",
	}
}

func TestProofreadRunOwnerIsolation(t *testing.T) {
	ctx, repo, run, ownerID, otherID := proofreadFixture(t)
	if got, err := repo.GetForUser(ctx, run.ID, ownerID); err != nil || got != run {
		t.Fatalf("owner could not read run: %+v, %v", got, err)
	}
	if _, err := repo.GetForUser(ctx, run.ID, otherID); !errors.Is(err, ErrProofreadRunNotFound) {
		t.Fatalf("another reader could read run: %v", err)
	}
	if _, err := repo.Create(ctx, ownerID, 11, 20); !errors.Is(err, ErrProofreadRunNotFound) {
		t.Fatalf("chapter accepted for another work: %v", err)
	}
	if _, err := repo.IssuePairCode(ctx, run.ID, otherID); !errors.Is(err, ErrProofreadRunNotFound) {
		t.Fatalf("another reader could issue a pair code: %v", err)
	}
}

func TestProofreadFindingsIdempotent(t *testing.T) {
	ctx, repo, run, _, _ := proofreadFixture(t)
	page := proofreadFindings(t, ctx, repo, run.ID)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(proofreadBase)))
	if page.BaseMarkdown != proofreadBase || page.BaseSHA256 != hash || page.AnalysisStatus != models.ProofreadAnalysisPending {
		t.Fatalf("wrong initial page snapshot: %+v", page)
	}
	finding := proofreadCandidate()
	if err := repo.PutFindings(ctx, run.ID, 30, hash, []models.ProofreadFinding{finding}); err != nil {
		t.Fatal(err)
	}
	first := proofreadFindings(t, ctx, repo, run.ID)
	if len(first.Findings) != 1 || first.AnalysisStatus != models.ProofreadAnalysisAnalyzed {
		t.Fatalf("wrong first upload: %+v", first)
	}
	finding.New = "слон"
	if err := repo.PutFindings(ctx, run.ID, 30, hash, []models.ProofreadFinding{finding}); err != nil {
		t.Fatal(err)
	}
	second := proofreadFindings(t, ctx, repo, run.ID)
	if len(second.Findings) != 1 || second.Findings[0].ID != first.Findings[0].ID ||
		second.Findings[0].New != "кит" || second.Findings[0].Decision != models.FindingPending {
		t.Fatalf("duplicate upload changed stored finding: %+v", second.Findings)
	}
}

func TestProofreadRejectsWrongBaseSpanAndPage(t *testing.T) {
	ctx, repo, run, _, _ := proofreadFixture(t)
	hash := proofreadFindings(t, ctx, repo, run.ID).BaseSHA256
	finding := proofreadCandidate()
	if err := repo.PutFindings(ctx, run.ID, 32, hash, nil); !errors.Is(err, ErrProofreadRunNotFound) {
		t.Fatalf("outside page accepted: %v", err)
	}
	if err := repo.PutFindings(ctx, run.ID, 30, "stale", nil); !errors.Is(err, ErrProofreadBaseMismatch) {
		t.Fatalf("bad hash accepted: %v", err)
	}
	bad := finding
	bad.CandidateKey, bad.StartRune = "bad-span", 3
	if err := repo.PutFindings(ctx, run.ID, 30, hash, []models.ProofreadFinding{finding, bad}); !errors.Is(err, ErrProofreadInvalidFinding) {
		t.Fatalf("bad span accepted: %v", err)
	}
	page := proofreadFindings(t, ctx, repo, run.ID)
	if len(page.Findings) != 0 || page.AnalysisStatus != models.ProofreadAnalysisPending {
		t.Fatalf("invalid upload changed page: %+v", page)
	}
}

func TestProofreadDecisionPersists(t *testing.T) {
	ctx, repo, run, ownerID, otherID := proofreadFixture(t)
	hash := proofreadFindings(t, ctx, repo, run.ID).BaseSHA256
	candidate := proofreadCandidate()
	if err := repo.PutFindings(ctx, run.ID, 30, hash, []models.ProofreadFinding{candidate}); err != nil {
		t.Fatal(err)
	}
	id := proofreadFindings(t, ctx, repo, run.ID).Findings[0].ID
	if err := repo.SetDecision(ctx, run.ID, id, otherID, models.FindingRejected, ""); !errors.Is(err, ErrProofreadRunNotFound) {
		t.Fatalf("another reader could decide: %v", err)
	}
	if err := repo.SetDecision(ctx, run.ID, id, ownerID, models.FindingAccepted, "кошка"); err != nil {
		t.Fatal(err)
	}
	if err := repo.PutFindings(ctx, run.ID, 30, hash, []models.ProofreadFinding{candidate}); err != nil {
		t.Fatal(err)
	}
	finding := proofreadFindings(t, ctx, repo, run.ID).Findings[0]
	if finding.Decision != models.FindingAccepted || finding.Replacement != "кошка" {
		t.Fatalf("accepted decision did not survive retry: %+v", finding)
	}
	if err := repo.SetDecision(ctx, run.ID, id, ownerID, models.FindingAccepted, ""); err != nil {
		t.Fatal(err)
	}
	finding = proofreadFindings(t, ctx, repo, run.ID).Findings[0]
	if finding.Decision != models.FindingAccepted || finding.Replacement != "" {
		t.Fatalf("accepted deletion did not persist: %+v", finding)
	}
}

func TestProofreadPageFailureCanBeRetried(t *testing.T) {
	ctx, repo, run, _, _ := proofreadFixture(t)
	hash := proofreadFindings(t, ctx, repo, run.ID).BaseSHA256
	if err := repo.SetPageAnalysisError(ctx, run.ID, 30, hash, "provider timed out"); err != nil {
		t.Fatal(err)
	}
	page := proofreadFindings(t, ctx, repo, run.ID)
	if page.AnalysisStatus != models.ProofreadAnalysisFailed || page.AnalysisError != "provider timed out" {
		t.Fatalf("page failure did not persist: %+v", page)
	}
	if err := repo.PutFindings(ctx, run.ID, 30, hash, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPageAnalysisError(ctx, run.ID, 30, hash, "late failure"); err != nil {
		t.Fatal(err)
	}
	page = proofreadFindings(t, ctx, repo, run.ID)
	if page.AnalysisStatus != models.ProofreadAnalysisAnalyzed || page.AnalysisError != "" {
		t.Fatalf("retry did not complete page: %+v", page)
	}
}

func TestProofreadPairCodeSingleUse(t *testing.T) {
	ctx, repo, run, ownerID, _ := proofreadFixture(t)
	otherRun, err := repo.Create(ctx, ownerID, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	code, err := repo.IssuePairCode(ctx, run.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	// Two helpers racing for the same code must produce exactly one token.
	type paired struct {
		run   models.ProofreadRun
		token string
		err   error
	}
	results := make(chan paired, 2)
	for range 2 {
		go func() {
			got, token, err := repo.ConsumePairCode(ctx, code)
			results <- paired{got, token, err}
		}()
	}
	var token string
	for range 2 {
		result := <-results
		if errors.Is(result.err, ErrProofreadCredential) {
			continue
		}
		if result.err != nil || result.run != run || result.token == "" || token != "" {
			t.Fatalf("unexpected pair result: %+v, second success: %t", result, token != "")
		}
		token = result.token
	}
	if token == "" {
		t.Fatal("neither helper could pair")
	}
	if got, err := repo.GetForWorker(ctx, run.ID, token); err != nil || got != run {
		t.Fatalf("token could not access its run: %+v, %v", got, err)
	}
	if _, err := repo.GetForWorker(ctx, otherRun.ID, token); !errors.Is(err, ErrProofreadCredential) {
		t.Fatalf("token allowed another run: %v", err)
	}
}

func TestProofreadPairCredentialsExpireAndAreHashed(t *testing.T) {
	ctx, repo, run, ownerID, _ := proofreadFixture(t)
	code, err := repo.IssuePairCode(ctx, run.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	pool := testPool(t)
	var storedHash []byte
	var expiresInFuture bool
	if err := pool.QueryRow(ctx, `SELECT pair_code_hash, pair_code_expires_at > now()
		FROM proofread_runs WHERE id = $1`, run.ID).Scan(&storedHash, &expiresInFuture); err != nil {
		t.Fatal(err)
	}
	if expected := sha256.Sum256([]byte(code)); string(storedHash) != string(expected[:]) || !expiresInFuture {
		t.Fatal("pair code not stored as an expiring hash")
	}
	if _, err := pool.Exec(ctx, `UPDATE proofread_runs SET pair_code_expires_at = now() WHERE id = $1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ConsumePairCode(ctx, code); !errors.Is(err, ErrProofreadCredential) {
		t.Fatalf("expired code accepted: %v", err)
	}
	code, err = repo.IssuePairCode(ctx, run.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := repo.ConsumePairCode(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT worker_token_hash, worker_token_expires_at > now()
		FROM proofread_runs WHERE id = $1`, run.ID).Scan(&storedHash, &expiresInFuture); err != nil {
		t.Fatal(err)
	}
	if expected := sha256.Sum256([]byte(token)); string(storedHash) != string(expected[:]) || !expiresInFuture {
		t.Fatal("worker token not stored as an expiring hash")
	}
	if _, err := pool.Exec(ctx, `UPDATE proofread_runs SET worker_token_expires_at = now() WHERE id = $1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetForWorker(ctx, run.ID, token); !errors.Is(err, ErrProofreadCredential) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestLatestProofreadRunForUser(t *testing.T) {
	ctx, repo, first, ownerID, otherID := proofreadFixture(t)
	if _, err := repo.LatestForUser(ctx, otherID, first.WorkID, first.ChapterID); !errors.Is(err, ErrProofreadRunNotFound) {
		t.Fatalf("foreign run: %v", err)
	}
	newest, err := repo.Create(ctx, ownerID, first.WorkID, first.ChapterID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, otherID, first.WorkID, first.ChapterID); err != nil {
		t.Fatal(err)
	}
	got, err := repo.LatestForUser(ctx, ownerID, first.WorkID, first.ChapterID)
	if err != nil || got.ID != newest.ID {
		t.Fatalf("latest = %+v, %v", got, err)
	}
}
