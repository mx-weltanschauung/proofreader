package api

import (
	"strings"
	"testing"

	"proofreader/internal/models"
)

func TestWorkerRejectsBadSpan(t *testing.T) {
	f := models.ProofreadFinding{CandidateKey: "x", StartRune: 2, EndRune: 3, Old: "а", New: "я", Source: "model", Reason: "скан"}
	if !validWorkerFinding([]rune("а б а"), models.ProofreadFinding{CandidateKey: "x", StartRune: 4, EndRune: 5, Old: "а", New: "я", Source: "model", Reason: "скан"}) {
		t.Fatal("valid repeated rune rejected")
	}
	if validWorkerFinding([]rune("а б а"), f) {
		t.Fatal("wrong original accepted")
	}
	f.Old = "б"
	f.Source = "unknown"
	if validWorkerFinding([]rune("а б а"), f) {
		t.Fatal("unknown source accepted")
	}
}

func TestWorkerValidatesUploadBeforeSaving(t *testing.T) {
	f := newRunHTTPFixture(t)
	const candidate = `{"candidate_key":"x","start_rune":0,"end_rune":9,"old":"секретный","new":"иной","source":"model","reason":"скан","scan_bbox":[0,0,0.5,0.5]}`
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"valid", candidate, 204},
		{"bad original", strings.Replace(candidate, "секретный", "сырой", 1), 422},
		{"wrong range", strings.Replace(candidate, "end_rune\":9", "end_rune\":4", 1), 422},
		{"missing start", strings.Replace(candidate, "\"start_rune\":0,", "", 1), 422},
		{"missing replacement", strings.Replace(candidate, "\"new\":\"иной\",", "", 1), 422},
		{"unknown source", strings.Replace(candidate, "model", "other", 1), 422},
		{"user decision", strings.Replace(candidate, "\"source\"", "\"decision\":\"accepted\",\"source\"", 1), 400},
		{"invalid coordinates", strings.Replace(candidate, "0.5,0.5", "3,2", 1), 422},
		{"duplicate key", candidate + "," + candidate, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.call("PUT", "proofread-worker/runs/7/pages/1001/findings", "worker-token", `{"base_sha256":"hash","findings":[`+tc.body+`]}`)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	if f.runs.uploads != 1 {
		t.Fatal(f.runs.uploads)
	}
}
