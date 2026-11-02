package telemetry

import (
	"context"
	"dorm-hub/internal/platform/mail"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"strconv"
	"time"
)

type Metrics struct {
	DatabaseUp      prometheus.Gauge
	Maintenance     prometheus.Gauge
	Registry        *prometheus.Registry
	Requests        *prometheus.CounterVec
	Duration        *prometheus.HistogramVec
	SMTP            *prometheus.CounterVec
	SMTPDuration    *prometheus.HistogramVec
	AnalyticsErrors prometheus.Counter
}

func New() *Metrics {
	m := &Metrics{
		Registry:    prometheus.NewRegistry(),
		DatabaseUp:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "dormhub_database_up", Help: "Whether the last metrics scrape could query PostgreSQL."}),
		Maintenance: prometheus.NewGauge(prometheus.GaugeOpts{Name: "dormhub_maintenance_enabled", Help: "Whether planned maintenance is enabled."}),
		Requests:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dormhub_http_requests_total", Help: "HTTP requests by bounded route, method and status."}, []string{"route", "method", "status"}),
		// Include millisecond buckets for fast API handlers. Keep metric/label names
		// unchanged so the existing Prometheus history remains usable.
		Duration:        prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "dormhub_http_request_duration_seconds", Help: "HTTP request duration.", Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"route", "method"}),
		SMTP:            prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dormhub_smtp_messages_total", Help: "SMTP send attempts, not confirmed mailbox delivery."}, []string{"purpose", "result"}),
		SMTPDuration:    prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "dormhub_smtp_duration_seconds", Help: "SMTP send attempt duration.", Buckets: []float64{.1, .5, 1, 2.5, 5, 10}}, []string{"purpose"}),
		AnalyticsErrors: prometheus.NewCounter(prometheus.CounterOpts{Name: "dormhub_analytics_errors_total", Help: "Failed optional analytics writes."}),
	}
	m.Registry.MustRegister(m.Maintenance, m.DatabaseUp, m.Requests, m.Duration, m.SMTP, m.SMTPDuration, m.AnalyticsErrors, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	for _, purpose := range []string{"login", "deletion"} {
		for _, result := range []string{"success", "error"} {
			m.SMTP.WithLabelValues(purpose, result).Add(0)
		}
	}
	return m
}
func (m *Metrics) ObserveHTTP(method, route string, status int, elapsed time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	if method != "GET" && method != "HEAD" && method != "POST" && method != "PUT" && method != "DELETE" && method != "OPTIONS" {
		method = "OTHER"
	}
	m.Requests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	m.Duration.WithLabelValues(route, method).Observe(elapsed.Seconds())
}
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{EnableOpenMetrics: true, MaxRequestsInFlight: 2, Timeout: 5 * time.Second})
}
func (m *Metrics) Database(db *pgxpool.Pool) {
	m.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "dormhub_db_connections_in_use", Help: "Acquired application database connections."}, func() float64 { return float64(db.Stat().AcquiredConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "dormhub_db_connections_max", Help: "Application database pool limit."}, func() float64 { return float64(db.Stat().MaxConns()) }))
}

type measuredMail struct {
	next    mail.Sender
	metrics *Metrics
}

func (m *Metrics) Mail(next mail.Sender) mail.Sender { return &measuredMail{next: next, metrics: m} }
func (s *measuredMail) send(ctx context.Context, purpose, to, code string) (err error) {
	start := time.Now()
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		}
		s.metrics.SMTP.WithLabelValues(purpose, result).Inc()
		s.metrics.SMTPDuration.WithLabelValues(purpose).Observe(time.Since(start).Seconds())
	}()
	if purpose == "deletion" {
		if d, ok := s.next.(interface {
			SendDeletionCode(context.Context, string, string) error
		}); ok {
			return d.SendDeletionCode(ctx, to, code)
		}
	}
	return s.next.SendCode(ctx, to, code)
}
func (s *measuredMail) SendCode(ctx context.Context, to, code string) error {
	return s.send(ctx, "login", to, code)
}
func (s *measuredMail) SendDeletionCode(ctx context.Context, to, code string) error {
	return s.send(ctx, "deletion", to, code)
}
