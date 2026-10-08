// Package metrics — здоровье читальни в формате Prometheus: запросы и
// задержки по шаблону маршрута, занятость ограничителей, пул pgx, процесс.
// Спека — docs/superpowers/specs/2026-09-29-traffic-and-health-metrics-design.md.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"proofreader/internal/limit"
	"proofreader/internal/middleware"
)

type Metrics struct {
	reg       *prometheus.Registry
	requests  *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	startedAt time.Time
}

func New() *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "Запросы по шаблону маршрута, методу и коду.",
		}, []string{"route", "method", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Время ответа по шаблону маршрута.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		}, []string{"route"}),
		startedAt: time.Now(),
	}
	m.reg.MustRegister(m.requests, m.duration,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}

// Middleware — счёт и время по ШАБЛОНУ маршрута mux, а не по пути: иначе
// каждый id глав стал бы своим рядом, и рядов было бы десятки тысяч.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := middleware.NewStatusWriter(w)
		next.ServeHTTP(sw, r)
		code := sw.Status
		if code == 0 {
			code = http.StatusOK
		}
		route := "unmatched"
		if cr := mux.CurrentRoute(r); cr != nil {
			if tpl, err := cr.GetPathTemplate(); err == nil {
				route = tpl
			}
		}
		m.requests.WithLabelValues(route, r.Method, strconv.Itoa(code)).Inc()
		m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

// Handler — вывод для Prometheus. Вешается на отдельный слушатель
// (METRICS_ADDR), не на роутер: так его не отдаст наружу ни один прокси.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// RegisterSlots — занятость ограничителя: len канала — сколько занято.
func (m *Metrics) RegisterSlots(pool string, s limit.Slots) {
	labels := prometheus.Labels{"pool": pool}
	m.reg.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "limiter_in_use",
			Help: "Занятые слоты ограничителя.", ConstLabels: labels},
			func() float64 { return float64(len(s)) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "limiter_capacity",
			Help: "Ёмкость ограничителя.", ConstLabels: labels},
			func() float64 { return float64(cap(s)) }),
	)
}

// RegisterPgx — состояние пула соединений с базой.
func (m *Metrics) RegisterPgx(p *pgxpool.Pool) {
	g := func(name, help string, f func(*pgxpool.Stat) float64) prometheus.Collector {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help},
			func() float64 { return f(p.Stat()) })
	}
	c := func(name, help string, f func(*pgxpool.Stat) float64) prometheus.Collector {
		return prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: help},
			func() float64 { return f(p.Stat()) })
	}
	m.reg.MustRegister(
		g("pgx_acquired_conns", "Занятые соединения.", func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) }),
		g("pgx_idle_conns", "Простаивающие соединения.", func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) }),
		g("pgx_max_conns", "Потолок пула.", func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) }),
		c("pgx_empty_acquire_total", "Сколько раз пришлось ждать соединения.",
			func(s *pgxpool.Stat) float64 { return float64(s.EmptyAcquireCount()) }),
		c("pgx_acquire_wait_seconds_total", "Суммарное ожидание соединения.",
			func(s *pgxpool.Stat) float64 { return s.AcquireDuration().Seconds() }),
	)
}

// RegisterCounterFunc — счётчик, который ведёт кто-то другой (конвейер
// статистики).
func (m *Metrics) RegisterCounterFunc(name, help string, f func() float64) {
	m.reg.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: help}, f))
}
