package telemetry

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPMetricsPreservePerEndpointCodesAndFastDurations(t *testing.T) {
	m := New()
	m.ObserveHTTP("GET", "GET /api/listings/{id}", 200, 1500*time.Microsecond)
	m.ObserveHTTP("GET", "GET /api/listings/{id}", 404, 4*time.Millisecond)
	m.ObserveHTTP("POST", "POST /api/auth/request", 500, 2*time.Second)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, line := range []string{
		`dormhub_http_requests_total{method="GET",route="GET /api/listings/{id}",status="200"} 1`,
		`dormhub_http_requests_total{method="GET",route="GET /api/listings/{id}",status="404"} 1`,
		`dormhub_http_requests_total{method="POST",route="POST /api/auth/request",status="500"} 1`,
		`dormhub_http_request_duration_seconds_bucket{method="GET",route="GET /api/listings/{id}",le="0.0025"} 1`,
		`dormhub_http_request_duration_seconds_bucket{method="GET",route="GET /api/listings/{id}",le="0.005"} 2`,
	} {
		if !strings.Contains(w.Body.String(), line) {
			t.Fatal("Missing metric", line, w.Body.String())
		}
	}
	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "dormhub_http_request_duration_seconds" {
			for _, metric := range f.Metric {
				route := ""
				for _, label := range metric.Label {
					if label.GetName() == "route" {
						route = label.GetValue()
					}
				}
				if route == "GET /api/listings/{id}" {
					if metric.Histogram.GetSampleCount() != 2 || metric.Histogram.GetSampleSum() != .0055 {
						t.Fatal("Wrong endpoint duration", metric)
					}
				}
			}
		}
	}
}

type failingMail struct{ deletion bool }

func (s *failingMail) SendCode(context.Context, string, string) error {
	return errors.New("private SMTP error")
}
func (s *failingMail) SendDeletionCode(context.Context, string, string) error {
	s.deletion = true
	return s.SendCode(context.Background(), "", "")
}
func TestSMTPAdapterAndPrivacyRemainCompatible(t *testing.T) {
	m := New()
	next := &failingMail{}
	sender := m.Mail(next)
	_ = sender.SendCode(context.Background(), "secret@example.com", "123456")
	_ = sender.(interface {
		SendDeletionCode(context.Context, string, string) error
	}).SendDeletionCode(context.Background(), "secret@example.com", "123456")
	if !next.deletion {
		t.Fatal("Deletion mail adapter changed")
	}
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	for _, value := range []string{"secret@example.com", "123456", "private SMTP error"} {
		if strings.Contains(w.Body.String(), value) {
			t.Fatal("Sensitive metric data", value)
		}
	}
}
