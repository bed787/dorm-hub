package integration

import (
	"bytes"
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
	"dorm-hub/web"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func legalRequestBody(email string) string {
	docs := web.CurrentLegal()
	data, _ := json.Marshal(map[string]any{"email": email, "consent": true, "rules": true, "consent_version": docs.ConsentVersion, "rules_version": docs.RulesVersion})
	return string(data)
}

func TestLegalAcceptanceEmailVerificationAndDeletion(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler := httpapi.New(cfg(), slog.New(slog.NewTextHandler(&logs, nil)), db, login, users.New(db), listings.New(db), locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), media.New(db, files))
	csrf := strings.Repeat("a", 64)
	do := func(method, path, body, token, userCSRF string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", cfg().BaseURL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "dormhub_login", Value: csrf})
		if token != "" {
			r.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
			r.Header.Set("X-CSRF-Token", userCSRF)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body.String()+"\n"+logs.String())
		}
	}
	email := "legal@students.example"
	good := legalRequestBody(email)
	for _, body := range []string{`{"email":"legal@students.example"}`, strings.Replace(good, `"consent":true`, `"consent":false`, 1), strings.Replace(good, `"rules":true`, `"rules":false`, 1)} {
		check(do("POST", "/api/auth/request", body, "", ""), 400)
	}
	stale := strings.Replace(good, web.CurrentLegal().ConsentVersion, strings.Repeat("0", 64), 1)
	check(do("POST", "/api/auth/request", stale, "", ""), 409)
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM login_codes`).Scan(&n); err != nil || n != 0 || mail.code != "" {
		t.Fatal("missing acceptance sent mail or stored email", n, err)
	}
	w := do("POST", "/api/auth/request", good, "", "")
	check(w, 200)
	if err = db.QueryRow(ctx, `SELECT count(*) FROM user_legal_acceptances`).Scan(&n); err != nil || n != 0 {
		t.Fatal("acceptance attached before email was verified", n, err)
	}
	wrong := "000000"
	if wrong == mail.code {
		wrong = "000001"
	}
	check(do("POST", "/api/auth/verify", `{"email":"`+email+`","code":"`+wrong+`"}`, "", ""), 400)
	if err = db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil || n != 0 {
		t.Fatal("wrong code created user", n, err)
	}
	before := time.Now().Add(-time.Minute)
	w = do("POST", "/api/auth/verify", `{"email":"`+email+`","code":"`+mail.code+`"}`, "", "")
	check(w, 200)
	var userID int64
	var consentText, rulesText, consentVersion, rulesVersion string
	var accepted, confirmed time.Time
	err = db.QueryRow(ctx, `SELECT user_id,consent_version,rules_version,consent_text,rules_text,accepted_at,confirmed_at FROM user_legal_acceptances`).Scan(&userID, &consentVersion, &rulesVersion, &consentText, &rulesText, &accepted, &confirmed)
	docs := web.CurrentLegal()
	if err != nil || consentVersion != docs.ConsentVersion || rulesVersion != docs.RulesVersion || consentText != docs.ConsentText || rulesText != docs.RulesText || accepted.Before(before) || confirmed.Before(accepted) {
		t.Fatal("incorrect acceptance evidence", err)
	}
	var payload struct {
		Data struct {
			CSRF string `json:"csrf"`
		}
	}
	if err = json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	token := ""
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == cfg().SessionCookie() {
			token = cookie.Value
		}
	}
	if token == "" {
		t.Fatal("no session cookie")
	}
	check(do("POST", "/api/me/deletion-code", `{}`, token, payload.Data.CSRF), 200)
	check(do("DELETE", "/api/me", `{"code":"`+mail.code+`"}`, token, payload.Data.CSRF), 200)
	if err = db.QueryRow(ctx, `SELECT (SELECT count(*) FROM users)+(SELECT count(*) FROM user_legal_acceptances)+(SELECT count(*) FROM login_codes)`).Scan(&n); err != nil || n != 0 {
		t.Fatal("account deletion retained acceptance/email", n, err)
	}

	// A valid legacy code cannot create an account through HTTP without acceptance.
	legacy := "legacy@students.example"
	if err = login.Request(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	check(do("POST", "/api/auth/verify", `{"email":"`+legacy+`","code":"`+mail.code+`"}`, "", ""), 400)
	if err = db.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil || n != 0 {
		t.Fatal("legacy code bypassed acceptance", n, err)
	}
}
