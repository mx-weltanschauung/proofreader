package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proofreader/internal/models"
	"proofreader/internal/repository"
	"proofreader/pkg/storage"
)

type proofreadFixtureStore struct {
	ProofreadRunStore
	run       models.ProofreadRun
	snapshots []models.ProofreadRunPage
	uploads   int
}

func (s *proofreadFixtureStore) GetForWorker(_ context.Context, id int64, token string) (models.ProofreadRun, error) {
	if id != s.run.ID || token != "worker-token" {
		return models.ProofreadRun{}, repository.ErrProofreadCredential
	}
	return s.run, nil
}
func (s *proofreadFixtureStore) GetForUser(_ context.Context, id, userID int64) (models.ProofreadRun, error) {
	if id != s.run.ID || userID != s.run.UserID {
		return models.ProofreadRun{}, repository.ErrProofreadRunNotFound
	}
	return s.run, nil
}
func (s *proofreadFixtureStore) ListPages(context.Context, int64) ([]models.ProofreadRunPage, error) {
	return s.snapshots, nil
}
func (s *proofreadFixtureStore) PutFindings(context.Context, int64, int64, string, []models.ProofreadFinding) error {
	s.uploads++
	return nil
}

type runHTTPFixture struct {
	handler    *ProofreadHandler
	router     http.Handler
	runs       *proofreadFixtureStore
	page       *models.Page
	visibility *fakeScanVisibility
	spans      spanStore
	reader     string
	store      *storage.MemoryStorage
}

func newRunHTTPFixture(t *testing.T) *runHTTPFixture {
	t.Helper()
	run := models.ProofreadRun{ID: 7, WorkID: 1, ChapterID: 1, UserID: 1}
	snapshots := []models.ProofreadRunPage{{PageID: 1001, PageNumber: 1, BaseMarkdown: "секретный текст", BaseSHA256: "hash", AnalysisStatus: models.ProofreadAnalysisFailed, Findings: []models.ProofreadFinding{{ID: 5, Reason: "секретная находка"}}}}
	pages := memPages(1, 1)
	pages[0].PreviewPath = "scan.png"
	f := &runHTTPFixture{runs: &proofreadFixtureStore{run: run, snapshots: snapshots}, page: pages[0], reader: readerToken(t), spans: spanStore{}, visibility: &fakeScanVisibility{}, store: storage.NewMemoryStorage()}
	if err := f.store.Put(context.Background(), "scan.png", strings.NewReader("png"), 3, "image/png"); err != nil {
		t.Fatal(err)
	}
	f.handler = &ProofreadHandler{runs: f.runs, chapters: viewChaptersFixture(), pages: NewViewPages(memPageStore(pages), f.spans), closed: &fakeClosedStore{}, scans: f.visibility, storage: f.store, ttl: time.Minute}
	f.router = newTestRouter(t).WithProofread(f.handler).Setup()
	return f
}
func (f *runHTTPFixture) call(method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}
func TestCreateRunRequiresReader(t *testing.T) {
	f := newRunHTTPFixture(t)
	for _, test := range []struct {
		token string
		want  int
	}{{"", 401}, {"worker-token", 401}, {tokenForRole(t, models.RoleEditor), 403}} {
		if w := f.call("POST", "proofread-runs", test.token, `{"work_id":1,"chapter_id":1}`); w.Code != test.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestRunRejectsForeignOwner(t *testing.T) {
	f := newRunHTTPFixture(t)
	f.runs.run.UserID = 999
	if w := f.call("GET", "proofread-runs/7", f.reader, ""); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestWorkerTokenCannotSubmit(t *testing.T) {
	f := newRunHTTPFixture(t)
	if w := f.call("POST", "works/1/pages/1001/suggestions", "worker-token", "{}"); w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestWorkerTokenCannotReadOtherRun(t *testing.T) {
	f := newRunHTTPFixture(t)
	for _, test := range []struct{ id, token string }{{"8", "worker-token"}, {"7", f.reader}} {
		if w := f.call("GET", "proofread-worker/runs/"+test.id+"/pages", test.token, ""); w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestWorkerRejectsHiddenChapter(t *testing.T) {
	f := newRunHTTPFixture(t)
	f.runs.run.ChapterID = 3
	for _, test := range []struct{ method, path, token string }{{"GET", "proofread-runs/7", f.reader}, {"GET", "proofread-worker/runs/7/pages", "worker-token"}, {"PUT", "proofread-worker/runs/7/pages/1001/findings", "worker-token"}} {
		if w := f.call(test.method, test.path, test.token, ""); w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func (f *runHTTPFixture) assertSkip(t *testing.T, reason string) {
	t.Helper()
	for _, test := range []struct{ path, token string }{{"proofread-runs/7", f.reader}, {"proofread-worker/runs/7/pages", "worker-token"}} {
		w := f.call("GET", test.path, test.token, "")
		b := w.Body.String()
		if w.Code != 200 || !strings.Contains(b, reason) || strings.Contains(b, "секрет") || strings.Contains(b, "scan.png") {
			t.Fatalf("%s -> %d %s", test.path, w.Code, b)
		}
	}
	w := f.call("PUT", "proofread-worker/runs/7/pages/1001/findings", "worker-token", `{"base_sha256":"hash","findings":[]}`)
	if w.Code != 422 || f.runs.uploads != 0 {
		t.Fatalf("upload %d %s", w.Code, w.Body.String())
	}
	w = f.call("PATCH", "proofread-runs/7/findings/5", f.reader, `{"decision":"accepted"}`)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestWorkerSkipsUnavailableScan(t *testing.T) {
	f := newRunHTTPFixture(t)
	f.visibility.spans = []models.PageSpan{{Start: 1, End: 1}}
	f.assertSkip(t, "Скан недоступен")
	f.visibility.spans = nil
	f.page.PreviewPath = "deleted.png"
	f.assertSkip(t, "Скан недоступен")
}
func TestWorkerSkipsBlankPage(t *testing.T) {
	f := newRunHTTPFixture(t)
	f.page.Status = models.PageStatusEmpty
	f.assertSkip(t, "Пустая полоса")
}
func TestProofreadSkipsHiddenPage(t *testing.T) {
	f := newRunHTTPFixture(t)
	f.spans[1] = []models.PageSpan{{Start: 1, End: 1}}
	f.assertSkip(t, "Полоса недоступна")
}
func TestWorkerCanRetryFailedPage(t *testing.T) {
	f := newRunHTTPFixture(t)
	for _, after := range []string{"0", "1", "0"} {
		w := f.call("GET", "proofread-worker/runs/7/pages?after="+after, "worker-token", "")
		if after == "1" {
			if w.Code != 204 {
				t.Fatal(w.Code)
			}
			continue
		}
		var p proofreadPage
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.AnalysisStatus != models.ProofreadAnalysisFailed || p.ScanURL != "mem://scan.png?ttl=1m0s" || p.BaseMarkdown != "секретный текст" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := f.call("PUT", "proofread-worker/runs/7/pages/1001/findings", "worker-token", `{"base_sha256":"hash","findings":[]}`)
	if w.Code != 204 || f.runs.uploads != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestWorkerReceivesFetchableSignedScan(t *testing.T) {
	object := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bucket/scan.png" {
			http.NotFound(w, r)
			return
		}
		if r.Method == "HEAD" {
			return
		}
		if r.URL.Query().Get("X-Amz-Signature") == "" || r.URL.Query().Get("X-Amz-Expires") != "60" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(403)
			return
		}
		io.WriteString(w, "image bytes")
	}))
	defer object.Close()
	backend, err := storage.NewS3Storage(storage.S3Config{Endpoint: object.URL, Bucket: "bucket", Region: "us-east-1", UsePathStyle: true, AccessKey: "test", SecretKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f := newRunHTTPFixture(t)
	f.handler.storage = backend
	w := f.call("GET", "proofread-worker/runs/7/pages", "worker-token", "")
	var p proofreadPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	response, err := object.Client().Get(p.ScanURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	b, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(b) != "image bytes" {
		t.Fatal(response.StatusCode, string(b), err)
	}
	f.visibility.whole = "work"
	f.assertSkip(t, "Скан недоступен")
}

func (s *proofreadFixtureStore) Create(_ context.Context, userID, workID, chapterID int64) (models.ProofreadRun, error) {
	if userID != s.run.UserID || workID != s.run.WorkID || chapterID != s.run.ChapterID {
		return models.ProofreadRun{}, repository.ErrProofreadRunNotFound
	}
	return s.run, nil
}
func (s *proofreadFixtureStore) IssuePairCode(ctx context.Context, id, userID int64) (string, error) {
	_, err := s.GetForUser(ctx, id, userID)
	return "one-code", err
}
func (s *proofreadFixtureStore) ConsumePairCode(_ context.Context, code string) (models.ProofreadRun, string, error) {
	if code != "one-code" {
		return models.ProofreadRun{}, "", repository.ErrProofreadCredential
	}
	return s.run, "worker-token", nil
}
func (s *proofreadFixtureStore) SetDecision(_ context.Context, id, findingID, userID int64, decision models.FindingDecision, replacement string) error {
	f := &s.snapshots[0].Findings[0]
	if id != s.run.ID || userID != s.run.UserID || findingID != f.ID {
		return repository.ErrProofreadRunNotFound
	}
	f.Decision = decision
	f.Replacement = replacement
	return nil
}
func TestProofreadReaderPairAndAccept(t *testing.T) {
	f := newRunHTTPFixture(t)
	finding := &f.runs.snapshots[0].Findings[0]
	finding.New = "слово"
	for _, tc := range []struct {
		method, path, token, body string
		status                    int
		contains                  string
	}{
		{"POST", "proofread-runs", f.reader, `{"work_id":1,"chapter_id":1}`, 201, `"id":7`},
		{"POST", "proofread-runs/7/pair-code", f.reader, "", 200, `"code":"one-code"`},
		{"POST", "proofread-worker/pair", "", `{"code":"one-code"}`, 200, `"token":"worker-token"`},
		{"GET", "proofread-runs/7", f.reader, "", 200, "секретная находка"},
		{"PATCH", "proofread-runs/7/findings/5", f.reader, `{"decision":"accepted"}`, 204, ""},
	} {
		w := f.call(tc.method, tc.path, tc.token, tc.body)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
	}
	if finding.Decision != models.FindingAccepted || finding.Replacement != "слово" {
		t.Fatal(finding)
	}
	w := f.call("PATCH", "proofread-runs/7/findings/5", f.reader, `{"decision":"accepted","replacement":""}`)
	if w.Code != 204 || finding.Replacement != "" {
		t.Fatal(w.Code, finding)
	}
}
