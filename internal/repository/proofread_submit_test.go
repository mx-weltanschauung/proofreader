package repository

import (
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
)

func TestSubmitReadyPage(t *testing.T) {
	ctx, repo, run, owner, other := proofreadFixture(t)
	page := proofreadFindings(t, ctx, repo, run.ID)
	first, second := proofreadCandidate(), proofreadCandidate()
	first.CandidateKey, first.StartRune, first.EndRune = "first", 0, 3
	if err := repo.PutFindings(ctx, run.ID, 30, page.BaseSHA256, []models.ProofreadFinding{first, second}); err != nil {
		t.Fatal(err)
	}
	findings := proofreadFindings(t, ctx, repo, run.ID).Findings
	accepted := map[int64]string{}
	decide := func(i int, d models.FindingDecision, s string) {
		t.Helper()
		if d == models.FindingAccepted {
			accepted[findings[i].ID] = s
		} else {
			delete(accepted, findings[i].ID)
		}
		if err := repo.SetDecision(ctx, run.ID, findings[i].ID, owner, d, s); err != nil {
			t.Fatal(err)
		}
	}
	submit := func(limit int, want string) models.PageSubmitResult {
		t.Helper()
		r, err := repo.SubmitReadyPage(ctx, run.ID, owner, 30, accepted, "ip", limit, time.Now().Add(-time.Hour))
		if err != nil || r.Status != want {
			t.Fatalf("%+v %v, want %s", r, err, want)
		}
		return r
	}
	if _, err := repo.SubmitReadyPage(ctx, run.ID, other, 30, accepted, "ip", 5, time.Now()); err == nil {
		t.Fatal("foreign owner")
	}
	decide(1, models.FindingAccepted, "кит")
	submit(1, "refresh_required")
	decide(0, models.FindingRejected, "")
	for _, bad := range []string{"кот", "к\u200bит", strings.Repeat("я", 20001)} {
		decide(1, models.FindingAccepted, bad)
		submit(1, "failed")
	}
	decide(1, models.FindingAccepted, "кит")
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := repo.pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE pages SET content_markdown='новый' WHERE id=30")
	submit(1, "stale")
	exec("UPDATE pages SET content_markdown=$1 WHERE id=30", proofreadBase)
	exec("INSERT INTO page_suggestions(page_id,base_markdown,proposed_markdown,ip_hash) VALUES(30,'a','b','ip')")
	submit(1, "rate_limited")
	var originalStatus string
	if err := repo.pool.QueryRow(ctx, "SELECT status FROM pages WHERE id=30").Scan(&originalStatus); err != nil {
		t.Fatal(err)
	}
	res := submit(2, "submitted")
	if res.SuggestionID == nil {
		t.Fatal("no suggestion")
	}
	var text, status string
	if err := repo.pool.QueryRow(ctx, "SELECT s.proposed_markdown,p.status FROM page_suggestions s JOIN pages p ON p.id=s.page_id WHERE s.id=$1", *res.SuggestionID).Scan(&text, &status); err != nil || text != "кот кит" || status != originalStatus {
		t.Fatal(text, status, err)
	}
	submit(2, "already_submitted")
	exec("DELETE FROM page_suggestions WHERE id=$1", *res.SuggestionID)
	if r := submit(2, "already_submitted"); r.SuggestionID != nil {
		t.Fatal(r)
	}
}

func TestSubmitReadyConcurrentRetries(t *testing.T) {
	ctx, repo, run, owner, _ := proofreadFixture(t)
	p := proofreadFindings(t, ctx, repo, run.ID)
	if err := repo.PutFindings(ctx, run.ID, p.PageID, p.BaseSHA256, []models.ProofreadFinding{proofreadCandidate()}); err != nil {
		t.Fatal(err)
	}
	f := proofreadFindings(t, ctx, repo, run.ID).Findings[0]
	if err := repo.SetDecision(ctx, run.ID, f.ID, owner, models.FindingAccepted, f.New); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		r   models.PageSubmitResult
		err error
	}
	const retries = 8
	results := make(chan outcome, retries)
	start := make(chan struct{})
	for i := 0; i < retries; i++ {
		go func() {
			<-start
			r, err := repo.SubmitReadyPage(ctx, run.ID, owner, p.PageID, map[int64]string{f.ID: f.New}, "same", retries, time.Now().Add(-time.Hour))
			results <- outcome{r, err}
		}()
	}
	close(start)
	counts := map[string]int{}
	var id int64
	for i := 0; i < retries; i++ {
		o := <-results
		if o.err != nil || o.r.SuggestionID == nil {
			t.Fatal(o)
		}
		if id != 0 && id != *o.r.SuggestionID {
			t.Fatal("different suggestions", id, *o.r.SuggestionID)
		}
		id = *o.r.SuggestionID
		counts[o.r.Status]++
	}
	if counts["submitted"] != 1 || counts["already_submitted"] != retries-1 {
		t.Fatal(counts)
	}
}

func TestSubmitReadyPartialLimit(t *testing.T) {
	ctx, repo, _, owner, _ := proofreadFixture(t)
	if _, err := repo.pool.Exec(ctx, "UPDATE pages SET content_markdown=$1 WHERE id=31", proofreadBase); err != nil {
		t.Fatal(err)
	}
	run, err := repo.Create(ctx, owner, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := repo.ListPages(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range snapshots {
		if err := repo.PutFindings(ctx, run.ID, p.PageID, p.BaseSHA256, []models.ProofreadFinding{proofreadCandidate()}); err != nil {
			t.Fatal(err)
		}
	}
	snapshots, err = repo.ListPages(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	previews := map[int64]map[int64]string{}
	for _, p := range snapshots {
		f := p.Findings[0]
		previews[p.PageID] = map[int64]string{f.ID: f.New}
		if err := repo.SetDecision(ctx, run.ID, f.ID, owner, models.FindingAccepted, f.New); err != nil {
			t.Fatal(err)
		}
	}
	onlyPreviewed, err := repo.ListSubmissionPages(ctx, run.ID, []int64{31})
	if err != nil || len(onlyPreviewed) != 1 || onlyPreviewed[0].PageID != 31 {
		t.Fatalf("preview of one ready page returned %+v: %v", onlyPreviewed, err)
	}
	for _, attempt := range []struct {
		limit int
		want  []string
	}{{1, []string{"submitted", "rate_limited"}}, {2, []string{"already_submitted", "submitted"}}} {
		pages, err := repo.ListSubmissionPages(ctx, run.ID, []int64{30, 31})
		if err != nil || len(pages) != 2 {
			t.Fatal(pages, err)
		}
		for i, p := range pages {
			res, err := repo.SubmitReadyPage(ctx, run.ID, owner, p.PageID, previews[p.PageID], "same", attempt.limit, time.Now().Add(-time.Hour))
			if err != nil || res.Status != attempt.want[i] {
				t.Fatal(res, err)
			}
		}
	}
}

func TestSubmitReadyChangedReplacementRequiresRefresh(t *testing.T) {
	ctx, repo, run, owner, _ := proofreadFixture(t)
	page := proofreadFindings(t, ctx, repo, run.ID)
	if err := repo.PutFindings(ctx, run.ID, page.PageID, page.BaseSHA256, []models.ProofreadFinding{proofreadCandidate()}); err != nil {
		t.Fatal(err)
	}
	f := proofreadFindings(t, ctx, repo, run.ID).Findings[0]
	for _, replacement := range []string{"кит", "лис"} {
		if err := repo.SetDecision(ctx, run.ID, f.ID, owner, models.FindingAccepted, replacement); err != nil {
			t.Fatal(err)
		}
	}
	result, err := repo.SubmitReadyPage(ctx, run.ID, owner, page.PageID, map[int64]string{f.ID: "кит"}, "ip", 10, time.Now().Add(-time.Hour))
	if err != nil || result.Status != "refresh_required" {
		t.Fatal(result, err)
	}
	var count int
	if err := repo.pool.QueryRow(ctx, "SELECT count(*) FROM page_suggestions WHERE page_id=$1", page.PageID).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
