package metrics

import (
	"sort"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// Health — выжимка для экрана /admin/stats: «с момента старта процесса».
type Health struct {
	StartedAt time.Time          `json:"started_at"`
	Routes    []RouteHealth      `json:"routes"`
	Values    map[string]float64 `json:"values"`
}

type RouteHealth struct {
	Route     string  `json:"route"`
	Requests  int64   `json:"requests"`
	Status5xx int64   `json:"status_5xx"`
	Status503 int64   `json:"status_503"`
	P50Ms     float64 `json:"p50_ms"`
	P95Ms     float64 `json:"p95_ms"`
}

// shownValues — какие одиночные ряды попадают в Values. Остальное (go_*
// кроме горутин) — шум для владельца, Prometheus их и так увидит.
func shownValue(name string) bool {
	switch {
	case strings.HasPrefix(name, "limiter_"), strings.HasPrefix(name, "pgx_"), strings.HasPrefix(name, "stats_"):
		return true
	}
	return name == "process_resident_memory_bytes" || name == "process_cpu_seconds_total" || name == "go_goroutines"
}

func (m *Metrics) Health() (*Health, error) {
	families, err := m.reg.Gather()
	if err != nil {
		return nil, err
	}
	h := &Health{StartedAt: m.startedAt, Values: map[string]float64{}}
	routes := map[string]*RouteHealth{}
	route := func(name string) *RouteHealth {
		if routes[name] == nil {
			routes[name] = &RouteHealth{Route: name}
		}
		return routes[name]
	}
	for _, f := range families {
		name := f.GetName()
		for _, mt := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range mt.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			switch {
			case name == "http_requests_total":
				rh := route(labels["route"])
				n := int64(mt.GetCounter().GetValue())
				rh.Requests += n
				if strings.HasPrefix(labels["code"], "5") {
					rh.Status5xx += n
				}
				if labels["code"] == "503" {
					rh.Status503 += n
				}
			case name == "http_request_duration_seconds":
				rh := route(labels["route"])
				rh.P50Ms = quantile(mt.GetHistogram(), 0.5) * 1000
				rh.P95Ms = quantile(mt.GetHistogram(), 0.95) * 1000
			case shownValue(name):
				key := name
				if p, ok := labels["pool"]; ok {
					key += `{pool="` + p + `"}`
				}
				h.Values[key] = value(mt)
			}
		}
	}
	for _, rh := range routes {
		h.Routes = append(h.Routes, *rh)
	}
	sort.Slice(h.Routes, func(i, j int) bool { return h.Routes[i].Requests > h.Routes[j].Requests })
	return h, nil
}

func value(mt *dto.Metric) float64 {
	switch {
	case mt.Gauge != nil:
		return mt.GetGauge().GetValue()
	case mt.Counter != nil:
		return mt.GetCounter().GetValue()
	}
	return 0
}

// quantile — оценка квантиля по корзинам гистограммы с линейной
// интерполяцией внутри корзины (тем же способом, что histogram_quantile).
func quantile(hist *dto.Histogram, q float64) float64 {
	total := float64(hist.GetSampleCount())
	if total == 0 {
		return 0
	}
	rank := q * total
	prevBound, prevCount := 0.0, 0.0
	for _, b := range hist.GetBucket() {
		count := float64(b.GetCumulativeCount())
		if count >= rank {
			if count == prevCount {
				return b.GetUpperBound()
			}
			return prevBound + (b.GetUpperBound()-prevBound)*(rank-prevCount)/(count-prevCount)
		}
		prevBound, prevCount = b.GetUpperBound(), count
	}
	return prevBound // хвост за последней корзиной: честнее верхней границы ничего нет
}
