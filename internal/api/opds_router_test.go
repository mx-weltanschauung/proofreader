package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"proofreader/internal/opds"
	"proofreader/internal/stats"
)

// HEAD наравне с GET: часть читалок и проверялок адреса пробует HEAD прежде
// GET, и mux без явного метода ответил бы 405.
func TestRouterServesOPDSWithHead(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte("<feed/>"))
	})
	r := createTestRouter().WithOPDS(h).Setup()
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(m, "/opds/works/1", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s /opds/works/1: %d", m, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/opds", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /opds: %d, ждали 405", rec.Code)
	}
}

func TestChannelRowCountsOPDSFeedsButNotSearch(t *testing.T) {
	cases := map[string]stats.Row{
		"/opds":                       {Channel: stats.ChannelOPDS, Kind: opds.KindRoot},
		"/opds/editions/7":            {Channel: stats.ChannelOPDS, Kind: opds.KindEdition, EntityID: 7},
		"/opds/works/252/chapters/10": {Channel: stats.ChannelOPDS, Kind: opds.KindChapter, WorkID: 252, EntityID: 10},
	}
	for path, want := range cases {
		got, ok := channelRow(httptest.NewRequest(http.MethodGet, path, nil))
		if !ok || got != want {
			t.Errorf("%s: %+v %v, ждали %+v", path, got, ok, want)
		}
	}
	for _, path := range []string{"/opds/search?q=xx", "/opds/works/x"} {
		if row, ok := channelRow(httptest.NewRequest(http.MethodGet, path, nil)); ok {
			t.Errorf("%s не должен считаться лентой: %+v", path, row)
		}
	}
}
