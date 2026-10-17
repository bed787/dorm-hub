package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportPolicyOnlyForPublicHTTPS(t *testing.T) {
	for _, secure := range []bool{true, false} {
		w := httptest.NewRecorder()
		h := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), slog.New(slog.NewTextHandler(io.Discard, nil)), secure)
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if got := w.Header().Get("Strict-Transport-Security"); (got != "") != secure {
			t.Fatalf("secure=%v HSTS=%q", secure, got)
		}
		if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("security headers missing")
		}
	}
}
