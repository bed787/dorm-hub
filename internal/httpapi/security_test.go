package httpapi

import (
	"dorm-hub/internal/auth"
	"dorm-hub/internal/config"
	"dorm-hub/internal/domain"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRF(t *testing.T) {
	token := strings.Repeat("a", 64)
	if !validCSRF(token, token) || validCSRF("", "") || validCSRF(token, strings.Repeat("b", 64)) {
		t.Fatal("CSRF validation failed")
	}
}
func TestJSONRejectsUnknownAndTrailing(t *testing.T) {
	for _, body := range []string{`{"name":"ok","admin":true}`, `{"name":"ok"} {"name":"again"}`} {
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		var in struct {
			Name string `json:"name"`
		}
		if err := decode(httptest.NewRecorder(), request, &in); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unsafe JSON accepted: %v", err)
		}
	}
}

func TestForwardedIPRequiresTrustedProxy(t *testing.T) {
	_, network, _ := net.ParseCIDR("127.0.0.1/32")
	a := &API{cfg: config.Config{TrustedProxies: []*net.IPNet{network}}}
	request := httptest.NewRequest("POST", "/", nil)
	request.RemoteAddr = "203.0.113.8:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := a.clientIP(request); got != "203.0.113.8" {
		t.Fatal("spoofed forwarded IP accepted", got)
	}
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "192.0.2.9, 198.51.100.4")
	if got := a.clientIP(request); got != "198.51.100.4" {
		t.Fatal("trusted chain not resolved from the right", got)
	}
}

func TestOriginForLANAndProduction(t *testing.T) {
	for _, tt := range []struct {
		name, env, base, target, origin string
		allowed                         bool
	}{
		{"LAN despite localhost default", "development", "http://localhost:8080", "http://192.168.31.42:8080/api/auth/request", "http://192.168.31.42:8080", true},
		{"localhost despite LAN setting", "development", "http://192.168.31.42:8080", "http://localhost:8080/api/auth/request", "http://localhost:8080", true},
		{"loopback", "development", "http://localhost:8080", "http://127.0.0.1:8080/api/auth/request", "http://127.0.0.1:8080", true},
		{"IPv6 loopback", "development", "http://localhost:8080", "http://[::1]:8080/api/auth/request", "http://[::1]:8080", true},
		{"other website", "development", "http://localhost:8080", "http://192.168.31.42:8080/api/auth/request", "https://evil.example", false},
		{"other LAN origin", "development", "http://localhost:8080", "http://192.168.31.42:8080/api/auth/request", "http://192.168.31.43:8080", false},
		{"other port", "development", "http://localhost:8080", "http://192.168.31.42:8080/api/auth/request", "http://192.168.31.42:9090", false},
		{"missing origin", "development", "http://localhost:8080", "http://192.168.31.42:8080/api/auth/request", "", false},
		{"null origin", "development", "http://localhost:8080", "http://192.168.31.42:8080/api/auth/request", "null", false},
		{"host spoof on public domain", "development", "http://localhost:8080", "http://evil.example/api/auth/request", "http://evil.example", false},
		{"public IP is not local", "development", "http://localhost:8080", "http://203.0.113.42:8080/api/auth/request", "http://203.0.113.42:8080", false},
		{"production rejects LAN", "production", "https://dorm.example", "http://192.168.31.42:8080/api/auth/request", "http://192.168.31.42:8080", false},
		{"production domain", "production", "https://dorm.example", "http://127.0.0.1:8080/api/auth/request", "https://dorm.example", true},
		{"production different origin", "production", "https://dorm.example", "https://other.example/api/auth/request", "https://other.example", false},
		{"default port normalization", "production", "https://dorm.example:443", "http://127.0.0.1:8080/api/auth/request", "https://dorm.example", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &API{cfg: config.Config{Env: tt.env, BaseURL: tt.base}}
			r := httptest.NewRequest("POST", tt.target, nil)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("X-Forwarded-Host", "localhost:8080")
			if got := a.allowedOrigin(r); got != tt.allowed {
				t.Fatalf("allowed=%v, want %v", got, tt.allowed)
			}
		})
	}
}

func TestLANOriginStillRequiresCSRF(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, tt := range []struct {
		name, origin, csrf string
		want               int
	}{
		{"local login request", "http://192.168.31.42:8080", token, 204},
		{"local request without CSRF", "http://192.168.31.42:8080", "", 403},
		{"foreign page with token", "https://evil.example", token, 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &API{cfg: config.Config{Env: "development", BaseURL: "http://localhost:8080"}, limiter: &limiter{}}
			called := false
			h := a.wrap(false, false, func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
				called = true
				w.WriteHeader(204)
				return nil
			})
			r := httptest.NewRequest("POST", "http://192.168.31.42:8080/api/auth/request", nil)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("X-CSRF-Token", tt.csrf)
			r.AddCookie(&http.Cookie{Name: a.loginCookie(), Value: token})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want || called != (tt.want == 204) {
				t.Fatalf("status %d, handler called %v", w.Code, called)
			}
		})
	}
}
