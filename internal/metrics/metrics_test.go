package metrics

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"proofreader/internal/limit"
)

func testRouter(m *Metrics) *mux.Router {
	r := mux.NewRouter()
	r.Use(m.Middleware)
	r.HandleFunc("/api/works/{id}", func(w http.ResponseWriter, r *http.Request) {}).Methods("GET")
	r.HandleFunc("/api/busy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}).Methods("GET")
	return r
}

func TestRouteLabelIsTemplate(t *testing.T) {
	m := New()
	r := testRouter(m)
	for i := 0; i < 1000; i++ {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", fmt.Sprintf("/api/works/%d", i), nil))
	}
	h, err := m.Health()
	if err != nil {
		t.Fatal(err)
	}
	var found *RouteHealth
	for i := range h.Routes {
		if strings.Contains(h.Routes[i].Route, "/api/works/") {
			if found != nil {
				t.Fatalf("больше одного ряда: %q и %q", found.Route, h.Routes[i].Route)
			}
			found = &h.Routes[i]
		}
	}
	if found == nil || found.Route != "/api/works/{id}" || found.Requests != 1000 {
		t.Fatalf("ряд: %+v", found)
	}
}

func TestHealthCounts503AndSlots(t *testing.T) {
	m := New()
	slots := limit.New(2)
	m.RegisterSlots("search", slots)
	rel, _ := slots.TryAcquire()
	defer rel()
	testRouter(m).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/busy", nil))

	h, _ := m.Health()
	var busy RouteHealth
	for _, rh := range h.Routes {
		if rh.Route == "/api/busy" {
			busy = rh
		}
	}
	if busy.Status503 != 1 || busy.Status5xx != 1 {
		t.Fatalf("503: %+v", busy)
	}
	if h.Values[`limiter_in_use{pool="search"}`] != 1 || h.Values[`limiter_capacity{pool="search"}`] != 2 {
		t.Fatalf("слоты: %v", h.Values)
	}
}

func TestHandlerExposesPrometheusText(t *testing.T) {
	m := New()
	testRouter(m).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/works/1", nil))
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), `http_requests_total{code="200",method="GET",route="/api/works/{id}"} 1`) {
		t.Fatalf("нет ряда в выводе:\n%s", body)
	}
}
