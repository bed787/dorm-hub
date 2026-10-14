package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dorm-hub/internal/auth"
)

func TestBootstrapReusesCookieAcrossTabs(t *testing.T) {
	a := &API{}
	token := strings.Repeat("a", 64)
	r := httptest.NewRequest("GET", "/api/auth/bootstrap", nil)
	r.AddCookie(&http.Cookie{Name: a.loginCookie(), Value: token})
	w := httptest.NewRecorder()
	if err := a.bootstrap(w, r, auth.Session{}); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Data struct {
			CSRF string `json:"csrf"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.CSRF != token || w.Result().Cookies()[0].Value != token {
		t.Fatal("existing guest token rotated")
	}
	invalid := httptest.NewRequest("GET", "/api/auth/bootstrap", nil)
	invalid.AddCookie(&http.Cookie{Name: a.loginCookie(), Value: "malformed"})
	w = httptest.NewRecorder()
	if err := a.bootstrap(w, invalid, auth.Session{}); err != nil {
		t.Fatal(err)
	}
	if !auth.ValidToken(w.Result().Cookies()[0].Value) {
		t.Fatal("malformed cookie was reused")
	}
}

func TestWriteLimitsArePerAccountAndOperation(t *testing.T) {
	a := &API{limiter: &limiter{}}
	r := httptest.NewRequest("POST", "/api/reports", nil)
	r.Pattern = "POST /api/reports"
	for i := 0; i < 10; i++ {
		if !a.allowMutation(r, 1) {
			t.Fatal("report budget exhausted early")
		}
	}
	if a.allowMutation(r, 1) {
		t.Fatal("report spam accepted")
	}
	if !a.allowMutation(r, 2) {
		t.Fatal("another account shares the report limit")
	}
	r.Pattern = "POST /api/listings/{id}/photos"
	if !a.allowMutation(r, 1) {
		t.Fatal("photo uploads share the report limit")
	}
	a.limiter.items["report:1"] = bucket{expires: time.Now().Add(-time.Second), count: 10}
	r.Pattern = "POST /api/reports"
	if !a.allowMutation(r, 1) {
		t.Fatal("expired budget did not reset")
	}
}
