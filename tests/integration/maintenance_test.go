package integration

import (
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/favorites"
	"dorm-hub/internal/httpapi"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/locations"
	"dorm-hub/internal/media"
	"dorm-hub/internal/moderation"
	"dorm-hub/internal/platform/filestore"
	"dorm-hub/internal/stars"
	"dorm-hub/internal/users"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaintenanceAccessAndRecovery(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = db.Exec(ctx, `UPDATE site_settings SET maintenance=false`) })
	member := account(t, db, "member@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	account(t, db, "otheradmin@students.example", true)
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	makeHandler := func() http.Handler {
		return httpapi.New(cfg(), slog.New(slog.NewTextHandler(io.Discard, nil)), db, login, users.New(db), listings.New(db), locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), media.New(db, files))
	}
	handler := makeHandler()
	makeSession := func(id int64) (string, string) {
		token, _ := auth.Token()
		csrf, _ := auth.Token()
		if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, auth.Hash(token), id, csrf); err != nil {
			t.Fatal(err)
		}
		return token, csrf
	}
	memberToken, memberCSRF := makeSession(member)
	adminToken, adminCSRF := makeSession(admin)
	guestCSRF := strings.Repeat("c", 64)
	do := func(method, path, body, token, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("Origin", cfg().BaseURL)
		r.Header.Set("X-CSRF-Token", csrf)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
		}
		r.AddCookie(&http.Cookie{Name: "dormhub_login", Value: guestCSRF})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String())
		}
	}
	path := "/api/admin/site-settings"
	check(do("GET", path, "", "", ""), 401)
	check(do("GET", path, "", memberToken, ""), 403)
	check(do("PUT", path, `{"maintenance":true}`, memberToken, memberCSRF), 403)
	check(do("PUT", path, `{"maintenance":true}`, adminToken, "wrong"), 403)
	for _, body := range []string{`{}`, `{"maintenance":null}`, `{"maintenance":"true"}`, `{"maintenance":true,"extra":1}`} {
		check(do("PUT", path, body, adminToken, adminCSRF), 400)
	}
	check(do("PUT", path, `{"maintenance":true}`, adminToken, adminCSRF), 200)

	// Rebuilding the handler simulates a new app instance; no in-memory flag is used.
	handler = makeHandler()
	w := do("GET", "/", "", "", "")
	check(w, 503)
	if !strings.Contains(w.Body.String(), "Технические работы") || w.Header().Get("Retry-After") == "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing maintenance page/headers")
	}
	w = do("HEAD", "/", "", "", "")
	check(w, 503)
	if w.Body.Len() != 0 {
		t.Fatal("HEAD returned a body")
	}
	for _, path := range []string{"/api/me", "/api/listings", "/api/catalog", "/api/photos/123", "/api/admin/site-settings"} {
		w = do("GET", path, "", memberToken, "")
		check(w, 503)
		if !strings.Contains(w.Body.String(), `"code":"maintenance"`) {
			t.Fatal("API did not identify maintenance")
		}
	}
	check(do("POST", "/api/listings", `{}`, memberToken, memberCSRF), 503)
	for _, path := range []string{"/health", "/ready", "/assets/app.js", "/admin-login", "/api/auth/bootstrap"} {
		check(do("GET", path, "", "", ""), 200)
	}
	check(do("GET", "/", "", adminToken, ""), 200)
	check(do("GET", "/api/listings", "", adminToken, ""), 200)
	check(do("GET", path, "", adminToken, ""), 200)
	check(do("PUT", path, `{"maintenance":false}`, adminToken, "wrong"), 403)

	// Ordinary visitors cannot request codes, register, or redeem codes while closed.
	check(do("POST", "/api/auth/request", `{"email":"new@students.example"}`, "", guestCSRF), 503)
	check(do("POST", "/api/auth/verify", `{"email":"new@students.example","code":"123456"}`, "", guestCSRF), 503)
	var count int
	if err = db.QueryRow(ctx, `SELECT (SELECT count(*) FROM login_codes WHERE email='new@students.example')+(SELECT count(*) FROM users WHERE email='new@students.example')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("maintenance login wrote visitor data", count, err)
	}

	// Both configured administrators and existing database administrators can recover access.
	for _, email := range []string{"admin@students.example", "otheradmin@students.example"} {
		body, _ := json.Marshal(map[string]string{"email": email})
		check(do("POST", "/api/auth/request", string(body), "", guestCSRF), 200)
		mail.mu.Lock()
		code := mail.code
		mail.mu.Unlock()
		body, _ = json.Marshal(map[string]string{"email": email, "code": code})
		w = do("POST", "/api/auth/verify", string(body), "", guestCSRF)
		check(w, 200)
		found := false
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == cfg().SessionCookie() {
				found = true
				check(do("GET", path, "", cookie.Value, ""), 200)
			}
		}
		if !found {
			t.Fatal("admin recovery did not create a session")
		}
	}
	check(do("PUT", path, `{"maintenance":false}`, adminToken, adminCSRF), 200)
	check(do("GET", "/", "", "", ""), 200)
	check(do("GET", "/api/listings", "", memberToken, ""), 200)
	check(do("POST", "/api/auth/request", `{"email":"new@students.example"}`, "", guestCSRF), 200)
}
