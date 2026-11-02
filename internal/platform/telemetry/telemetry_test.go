package telemetry

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeMail struct {
	fail     bool
	deletion bool
}

func (s *fakeMail) SendCode(context.Context, string, string) error {
	if s.fail {
		return errors.New("password must not be a label")
	}
	return nil
}
func (s *fakeMail) SendDeletionCode(context.Context, string, string) error {
	s.deletion = true
	return s.SendCode(context.Background(), "", "")
}
func TestMetricsCountSMTPAndHTTPWithoutPersonalLabels(t *testing.T) {
	m := New()
	next := &fakeMail{}
	sender := m.Mail(next)
	if err := sender.SendCode(context.Background(), "private@example.com", "123456"); err != nil {
		t.Fatal(err)
	}
	next.fail = true
	if err := sender.(interface {
		SendDeletionCode(context.Context, string, string) error
	}).SendDeletionCode(context.Background(), "private@example.com", "123456"); err == nil || !next.deletion {
		t.Fatal("deletion adapter lost")
	}
	m.ObserveHTTP("GET", "GET /api/listings/{id}", 500, time.Millisecond)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()
	for _, bad := range []string{"private@example.com", "123456", "password must"} {
		if strings.Contains(body, bad) {
			t.Fatal("personal metric label", bad)
		}
	}
	for _, want := range []string{`dormhub_smtp_messages_total{purpose="login",result="success"} 1`, `dormhub_smtp_messages_total{purpose="deletion",result="error"} 1`, `status="500"`} {
		if !strings.Contains(body, want) {
			t.Fatal("metric missing", want)
		}
	}
}
