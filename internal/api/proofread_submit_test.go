package api

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"proofreader/internal/models"
)

type submitFixture struct {
	*proofreadFixtureStore
	calls    []int64
	limits   []int
	hashes   []string
	previews []map[int64]string
}

func (s *submitFixture) ListSubmissionPages(ctx context.Context, id int64, pageIDs []int64) ([]models.ProofreadRunPage, error) {
	var result []models.ProofreadRunPage
	for _, p := range s.snapshots {
		if slices.Contains(pageIDs, p.PageID) {
			result = append(result, p)
		}
	}
	return result, nil
}
func (s *submitFixture) SubmitReadyPage(_ context.Context, run, owner, page int64, accepted map[int64]string, hash string, limit int, since time.Time) (models.PageSubmitResult, error) {
	s.calls = append(s.calls, page)
	s.previews = append(s.previews, accepted)
	s.limits = append(s.limits, limit)
	s.hashes = append(s.hashes, hash)
	status := "submitted"
	if page == 1003 && limit == 1 {
		status = "rate_limited"
	}
	return models.PageSubmitResult{PageID: page, Status: status}, nil
}
func TestProofreadSubmitUsesCurrentLimitAndSkipsUnavailableScan(t *testing.T) {
	f := newRunHTTPFixture(t)
	s := &submitFixture{proofreadFixtureStore: f.runs}
	f.handler.runs = s
	limit := &fakeSuggestionLimit{value: 1}
	f.handler.limit = limit
	pages := memPages(1, 3)
	for i, p := range pages {
		p.PreviewPath = "scan.png"
		if i == 0 {
			p.PreviewPath = "missing.png"
		}
	}
	s.snapshots = nil
	for _, p := range pages {
		s.snapshots = append(s.snapshots, models.ProofreadRunPage{PageID: p.ID, PageNumber: p.PageNumber, BaseMarkdown: p.ContentMarkdown})
	}
	f.handler.pages = NewViewPages(memPageStore(pages), f.spans)
	for _, test := range []struct {
		limit int
		last  string
	}{{1, "rate_limited"}, {20, "submitted"}} {
		limit.value = test.limit
		w := f.call("POST", "proofread-runs/7/submit", f.reader, `{"pages":[{"page_id":1001,"accepted":{"1":"a"}},{"page_id":1002,"accepted":{"2":"b"}},{"page_id":1003,"accepted":{"3":"c"}}]}`)
		var got struct{ Pages []models.PageSubmitResult }
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 || len(got.Pages) != 3 || got.Pages[0].Status != "failed" || got.Pages[1].Status != "submitted" || got.Pages[2].Status != test.last {
			t.Fatal(w.Code, w.Body.String(), err)
		}
		if len(s.calls) != 2 || s.calls[0] != 1002 || s.calls[1] != 1003 || s.limits[0] != test.limit || s.limits[1] != test.limit || s.hashes[0] != hashIP("192.0.2.1", f.handler.jwtSecret) {
			t.Fatal(s.calls, s.limits, s.hashes)
		}
		s.calls = nil
		s.limits = nil
		s.hashes = nil
	}
}
func TestProofreadSubmitRequiresReaderOwner(t *testing.T) {
	f := newRunHTTPFixture(t)
	for _, test := range []struct {
		token string
		want  int
	}{{"", 401}, {"worker-token", 401}, {tokenForRole(t, models.RoleEditor), 403}} {
		if w := f.call("POST", "proofread-runs/7/submit", test.token, ""); w.Code != test.want {
			t.Fatal(w.Code)
		}
	}
	f.runs.run.UserID = 999
	if w := f.call("POST", "proofread-runs/7/submit", f.reader, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestProofreadSubmitOnlyPreviewedPages(t *testing.T) {
	f := newRunHTTPFixture(t)
	s := &submitFixture{proofreadFixtureStore: f.runs}
	f.handler.runs = s
	f.handler.limit = &fakeSuggestionLimit{value: 20}
	pages := memPages(1, 2)
	s.snapshots = nil
	for _, p := range pages {
		p.PreviewPath = "scan.png"
		s.snapshots = append(s.snapshots, models.ProofreadRunPage{PageID: p.ID, PageNumber: p.PageNumber, BaseMarkdown: p.ContentMarkdown, Findings: []models.ProofreadFinding{{ID: p.ID, Decision: models.FindingAccepted, Replacement: "замена"}}})
	}
	f.handler.pages = NewViewPages(memPageStore(pages), f.spans)
	w := f.call("POST", "proofread-runs/7/submit", f.reader, `{"pages":[{"page_id":1001,"accepted":{"1001":"замена"}}]}`)
	if w.Code != 200 || len(s.calls) != 1 || s.calls[0] != 1001 || len(s.previews[0]) != 1 || s.previews[0][1001] != "замена" {
		t.Fatalf("reviewed only 1001; submitted %v (HTTP %d)", s.calls, w.Code)
	}
}

func TestProofreadSubmitMissingOrUnknownPreview(t *testing.T) {
	f := newRunHTTPFixture(t)
	s := &submitFixture{proofreadFixtureStore: f.runs}
	f.handler.runs = s
	for _, tc := range []struct {
		body string
		want int
	}{
		{"", 400},
		{`{"pages":[]}`, 400},
		{`{"pages":[{"page_id":1001,"accepted":{}}]}`, 400},
		{`{"pages":[{"page_id":1001,"accepted":{"1":"а"}},{"page_id":1001,"accepted":{"1":"б"}}]}`, 400},
		{`{"pages":[{"page_id":1001,"accepted":{"1":"а"}},{"page_id":99,"accepted":{"2":"б"}}]}`, 404},
	} {
		w := f.call("POST", "proofread-runs/7/submit", f.reader, tc.body)
		if w.Code != tc.want || len(s.calls) > 0 {
			t.Fatalf("HTTP %d calls %v", w.Code, s.calls)
		}
	}
}
