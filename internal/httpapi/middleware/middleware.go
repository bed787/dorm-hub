package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
func Wrap(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		idBytes := make([]byte, 12)
		_, _ = rand.Read(idBytes)
		id := hex.EncodeToString(idBytes)
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		response := &recorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic", "request_id", id, "panic_type", fmt.Sprintf("%T", recovered))
				if response.status == 0 {
					http.Error(response, "Внутренняя ошибка", 500)
				}
			}
			status := response.status
			if status == 0 {
				status = 200
			}
			logger.Info("http request", "request_id", id, "method", r.Method, "route", r.Pattern, "status", status, "duration_ms", time.Since(started).Milliseconds())
		}()
		next.ServeHTTP(response, r)
	})
}
