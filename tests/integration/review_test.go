package integration

import (
	"bytes"
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/favorites"
	"dorm-hub/internal/httpapi"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/locations"
	"dorm-hub/internal/media"
	"dorm-hub/internal/moderation"
	"dorm-hub/internal/platform/filestore"
	"dorm-hub/internal/stars"
	"dorm-hub/internal/users"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reviewAPI(t *testing.T, db *pgxpool.Pool, mail *mailbox) http.Handler {
	t.Helper()
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return httpapi.New(cfg(), slog.New(slog.NewTextHandler(io.Discard, nil)), db, auth.New(db, cfg(), mail), users.New(db), listings.New(db), locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), media.New(db, files))
}

func reviewRequest(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", cfg().BaseURL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", strings.Repeat("c", 64))
	r.AddCookie(&http.Cookie{Name: "dormhub_login", Value: strings.Repeat("c", 64)})
	if token != "" {
		r.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestReloadAndMaintenanceKeepMemberSession(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	mail := &mailbox{}
	if _, err := db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`) })
	login := auth.New(db, cfg(), mail)
	if err := login.Request(ctx, "member@students.example"); err != nil {
		t.Fatal(err)
	}
	token, session, err := login.Verify(ctx, "member@students.example", mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	handler := reviewAPI(t, db, mail)
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
		}
	}
	w := reviewRequest(handler, "GET", "/", "", token)
	check(w, 307)
	if w.Header().Get("Location") != "/app" || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), `id="landing"`) {
		t.Fatal("authenticated HTML flashed the landing")
	}
	for _, path := range []string{"/about", "/rules", "/privacy", "/consent"} {
		w = reviewRequest(handler, "GET", path, "", token)
		check(w, 200)
		if !strings.Contains(w.Body.String(), `href="/app#/feed">В ленту</a>`) || strings.Contains(w.Body.String(), `href="/login"`) {
			t.Fatal("public page forgot the existing session", path, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || len(w.Result().Cookies()) != 0 {
			t.Fatal("public page caches or replaces a session", path)
		}
		head := reviewRequest(handler, "HEAD", path, "", token)
		check(head, 200)
		if head.Body.Len() != 0 {
			t.Fatal("public HEAD has a body", path)
		}
		for _, guest := range []string{"", strings.Repeat("f", 64)} {
			w = reviewRequest(handler, "GET", path+"?authenticated=true", "", guest)
			check(w, 200)
			if !strings.Contains(w.Body.String(), `href="/login">Войти</a>`) || strings.Contains(w.Body.String(), `href="/app#/feed"`) {
				t.Fatal("guest received authenticated navigation", path)
			}
		}
	}
	check(reviewRequest(handler, "GET", "/api/me", "", token), 200)
	if _, err = db.Exec(ctx, `UPDATE site_settings SET maintenance=true,maintenance_reason='Проверка'`); err != nil {
		t.Fatal(err)
	}
	// A new handler models a deployment. Neither it nor maintenance may revoke
	// or overwrite the member's persisted session/cookie.
	handler = reviewAPI(t, db, mail)
	for _, path := range []string{"/", "/app", "/api/me", "/api/listings"} {
		w = reviewRequest(handler, "GET", path, "", token)
		check(w, 503)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("maintenance changed session cookies", path)
		}
	}
	if got, err := login.Authenticate(ctx, token); err != nil || got.User.ID != session.User.ID {
		t.Fatal("maintenance invalidated session", err)
	}
	if _, err = db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`); err != nil {
		t.Fatal(err)
	}
	handler = reviewAPI(t, db, mail)
	check(reviewRequest(handler, "GET", "/api/me", "", token), 200)
	check(reviewRequest(handler, "GET", "/", "", token), 307)
	check(reviewRequest(handler, "GET", "/app", "", token), 200)
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, session.User.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("session was recreated/lost", count, err)
	}
}

func TestDedicatedAdministratorLoginChecksRoleBeforeSending(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	mail := &mailbox{}
	handler := reviewAPI(t, db, mail)
	if _, err := db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`) })
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
		}
	}
	check(reviewRequest(handler, "POST", "/api/auth/admin/request", legalRequestBody("member@students.example"), ""), 403)
	if mail.code != "" {
		t.Fatal("administrator form sent mail to a regular member")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM login_codes`).Scan(&count); err != nil || count != 0 {
		t.Fatal("administrator form stored regular email", count, err)
	}
	// Even a valid ordinary OTP must not work on the administrator endpoint.
	check(reviewRequest(handler, "POST", "/api/auth/request", legalRequestBody("member@students.example"), ""), 200)
	body := `{"email":"member@students.example","code":"` + mail.code + `"}`
	check(reviewRequest(handler, "POST", "/api/auth/admin/verify", body, ""), 403)
	check(reviewRequest(handler, "POST", "/api/auth/verify", body, ""), 200)
	account(t, db, "otheradmin@students.example", true)
	if _, err := db.Exec(ctx, `UPDATE site_settings SET maintenance=true,maintenance_reason='Проверка'`); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"admin@students.example", "otheradmin@students.example"} {
		check(reviewRequest(handler, "POST", "/api/auth/admin/request", `{"email":"`+email+`"}`, ""), 200)
		// An administrator recovery OTP cannot register through the public
		// endpoint without a real acceptance, and no acceptance is fabricated.
		verifyBody := `{"email":"` + email + `","code":"` + mail.code + `"}`
		check(reviewRequest(handler, "POST", "/api/auth/verify", verifyBody, ""), 400)
		w := reviewRequest(handler, "POST", "/api/auth/admin/verify", `{"email":"`+email+`","code":"`+mail.code+`"}`, "")
		check(w, 200)
		var token string
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == cfg().SessionCookie() {
				token = cookie.Value
				if cookie.MaxAge < 29*24*3600 {
					t.Fatal("session cookie unexpectedly short")
				}
			}
		}
		if token == "" {
			t.Fatal("administrator cookie missing")
		}
		check(reviewRequest(handler, "GET", "/api/admin/site-settings", "", token), 200)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM user_legal_acceptances a JOIN users u ON u.id=a.user_id WHERE u.is_admin`).Scan(&count); err != nil || count != 0 {
		t.Fatal("administrator recovery fabricated a legal acceptance", count, err)
	}
	if _, err := db.Exec(ctx, `UPDATE users SET blocked=true WHERE email='admin@students.example'`); err != nil {
		t.Fatal(err)
	}
	check(reviewRequest(handler, "POST", "/api/auth/admin/request", legalRequestBody("admin@students.example"), ""), 403)
}

func TestPhotoDeleteRetriesDiskCleanupWithoutLosingDatabaseResult(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	dir := filepath.Join(t.TempDir(), "uploads")
	files, err := filestore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.New(db, files)
	p, err := photos.Upload(ctx, l.ID, owner, jpegData(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(dir, dir+"-offline"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dir, []byte("temporarily unavailable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = photos.Delete(ctx, p.ID, l.ID, owner); err != nil {
		t.Fatal("committed deletion reported as failure", err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM photos WHERE id=$1`, p.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("photo record remained", err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM pending_file_deletions`).Scan(&count); err != nil || count != 1 {
		t.Fatal("cleanup was not queued", err)
	}
	if err = os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(dir+"-offline", dir); err != nil {
		t.Fatal(err)
	}
	if err = photos.FlushDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = files.Open(p.ID + ".jpg"); !os.IsNotExist(err) {
		t.Fatal("queued file remained", err)
	}
}

func TestModeratorDeletionReleasesPhotoQuota(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.NewWithLimits(db, files, 1024*1024, 1)
	ads := listings.New(db)
	first, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	p, err := photos.Upload(ctx, first.ID, owner, jpegData(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = moderation.New(db).Action(ctx, admin, "listing", first.ID, "delete", "Объявление нарушает правила"); err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Get(ctx, first.ID, owner, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted ad still visible", err)
	}
	second, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = photos.Upload(ctx, second.ID, owner, jpegData(t)); err != nil {
		t.Fatal("deleted photo still consumes quota", err)
	}
	if err = photos.FlushDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = files.Open(p.ID + ".jpg"); !os.IsNotExist(err) {
		t.Fatal("deleted listing photo remained", err)
	}
}

func jpegData(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
