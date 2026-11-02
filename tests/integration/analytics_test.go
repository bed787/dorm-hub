package integration

import (
	"context"
	"dorm-hub/internal/analytics"
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func analyticsRequest(h http.Handler, method, path, body, token, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", cfg().BaseURL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAnalyticsDeduplicationMilestonesAndDeletion(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	a := analytics.New(db)
	ads := listings.New(db)
	owner := account(t, db, "owner@students.example", false)
	viewer := account(t, db, "viewer@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	l, err = ads.Review(ctx, l.ID, admin, l.Revision, true, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = a.Active(ctx, viewer); err != nil {
			t.Fatal(err)
		}
		for _, event := range []string{"view", "telegram"} {
			if err = a.Event(ctx, viewer, l.ID, event); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Self, admin and a channel absent from the listing must not add events.
	for _, id := range []int64{owner, admin} {
		if err = a.Event(ctx, id, l.ID, "view"); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.Active(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err = a.Event(ctx, viewer, l.ID, "vk"); err != nil {
		t.Fatal(err)
	}
	if err = a.Event(ctx, viewer, l.ID, "made-up"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("unbounded event accepted", err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO analytics_activity(day,user_id) VALUES((now() AT TIME ZONE 'Europe/Moscow')::date-1,$1),((now() AT TIME ZONE 'Europe/Moscow')::date-8,$1)`, viewer); err != nil {
		t.Fatal(err)
	}
	page, err := a.Summary(ctx, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.DAU != 1 || page.WAU != 1 || page.MAU != 1 || page.Views != 1 || page.Contacts != 1 || page.Publications != 1 || len(page.Series) != 7 || len(page.Listings) != 1 {
		t.Fatalf("wrong aggregate: %+v", page)
	}
	var first time.Time
	if err = db.QueryRow(ctx, `SELECT first_published_at FROM listings WHERE id=$1`, l.ID).Scan(&first); err != nil {
		t.Fatal(err)
	}
	// A second approval changes feed recency, but never the first publication.
	in := input()
	in.Revision = l.Revision
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	l, err = ads.Review(ctx, l.ID, admin, l.Revision, true, "")
	if err != nil {
		t.Fatal(err)
	}
	var again time.Time
	if err = db.QueryRow(ctx, `SELECT first_published_at FROM listings WHERE id=$1`, l.ID).Scan(&again); err != nil || !again.Equal(first) {
		t.Fatal("repeat moderation counted as first publication", err)
	}
	l, err = ads.TransitionWithOutcome(ctx, l.ID, owner, "complete", "success")
	if err != nil {
		t.Fatal(err)
	}
	page, err = a.Summary(ctx, 7, 0)
	if err != nil || page.Successful != 1 {
		t.Fatal("completion missing", page, err)
	}
	var success time.Time
	if err = db.QueryRow(ctx, `SELECT successful_at FROM listings WHERE id=$1`, l.ID).Scan(&success); err != nil {
		t.Fatal(err)
	}
	l, err = ads.Transition(ctx, l.ID, owner, "submit")
	if err != nil {
		t.Fatal(err)
	}
	l, err = ads.Review(ctx, l.ID, admin, l.Revision, true, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ads.TransitionWithOutcome(ctx, l.ID, owner, "complete", "success")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT successful_at FROM listings WHERE id=$1`, l.ID).Scan(&again); err != nil || !again.Equal(success) {
		t.Fatal("repeat success changes history", err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM users WHERE id=$1`, viewer); err != nil {
		t.Fatal(err)
	}
	page, err = a.Summary(ctx, 7, 0)
	if err != nil || page.DAU != 0 || page.MAU != 0 || page.Views != 0 || page.Contacts != 0 {
		t.Fatal("deleted viewer data survives", page, err)
	}
	if _, err = a.Summary(ctx, 365, 0); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("unbounded period", err)
	}
	if err = ads.Delete(ctx, l.ID, owner); err != nil {
		t.Fatal(err)
	}
	page, err = a.Summary(ctx, 7, 0)
	if err != nil || page.Publications != 0 || page.Successful != 0 {
		t.Fatal("deleted listing survives", page, err)
	}
}

func TestAnalyticsHTTPAccessAndRealBrowsing(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	owner := account(t, db, "owner@students.example", false)
	type credentials struct {
		token, csrf string
		id          int64
	}
	signIn := func(email string) credentials {
		t.Helper()
		if err := login.Request(ctx, email); err != nil {
			t.Fatal(err)
		}
		token, s, err := login.Verify(ctx, email, mail.code, false)
		if err != nil {
			t.Fatal(err)
		}
		return credentials{token, s.CSRF, s.User.ID}
	}
	viewer := signIn("viewer@students.example")
	admin := signIn("admin@students.example")
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	l, err = ads.Review(ctx, l.ID, admin.id, l.Revision, true, "")
	if err != nil {
		t.Fatal(err)
	}
	h := reviewAPI(t, db, mail)
	request := func(c credentials, method, path, body string) *httptest.ResponseRecorder {
		return analyticsRequest(h, method, path, body, c.token, c.csrf)
	}
	if w := request(credentials{}, "GET", "/api/admin/analytics", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(viewer, "GET", "/api/admin/analytics", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 3; i++ {
		if w := request(viewer, "GET", fmt.Sprintf("/api/listings/%d", l.ID), ""); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	body := fmt.Sprintf(`{"listing_id":%d,"event":"telegram"}`, l.ID)
	if w := request(viewer, "POST", "/api/analytics/events", body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := analyticsRequest(h, "POST", "/api/analytics/events", body, viewer.token, "bad"); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	w := request(admin, "GET", "/api/admin/analytics?days=7", "")
	var response struct {
		Data struct {
			analytics.Summary
			Monitoring bool `json:"monitoring_enabled"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	if response.Data.Views != 1 || response.Data.Contacts != 1 || response.Data.DAU != 1 || response.Data.Monitoring {
		t.Fatal("wrong dashboard JSON", w.Body.String())
	}
	// Application's public listener must never expose the private scrape endpoint.
	if w = request(admin, "GET", "/metrics", ""); w.Code != 404 {
		t.Fatal("metrics exposed publicly", w.Code)
	}
}

func TestMonitoringProxyRequiresAdminAndDoesNotForwardAppSecrets(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	signIn := func(email string) string {
		t.Helper()
		if err := login.Request(ctx, email); err != nil {
			t.Fatal(err)
		}
		token, _, err := login.Verify(ctx, email, mail.code, false)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	member, admin := signIn("member@students.example"), signIn("admin@students.example")
	var incoming http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		incoming = r.Header.Clone()
		w.Header().Set("Content-Security-Policy", "script-src 'nonce-test'")
		fmt.Fprint(w, "Grafana login")
	}))
	defer backend.Close()
	settings := cfg()
	settings.MonitoringURL = backend.URL
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := httpapi.New(settings, slog.New(slog.NewTextHandler(io.Discard, nil)), db, login, users.New(db), listings.New(db), locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), media.New(db, files))
	if w := analyticsRequest(h, "GET", "/monitoring/", "", "", ""); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if w := analyticsRequest(h, "GET", "/monitoring/", "", member, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("GET", "/monitoring/", nil)
	r.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: admin})
	r.AddCookie(&http.Cookie{Name: "grafana_session", Value: "grafana-only"})
	r.Header.Set("X-WEBAUTH-USER", "forged")
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "Grafana login" || w.Header().Get("Content-Security-Policy") != "script-src 'nonce-test'" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	if incoming.Get("Authorization") != "" || incoming.Get("X-WEBAUTH-USER") != "" || strings.Contains(incoming.Get("Cookie"), admin) || incoming.Get("Cookie") != "grafana_session=grafana-only" {
		t.Fatal("app identity leaked to Grafana", incoming)
	}
	r = httptest.NewRequest("POST", "/monitoring/login", strings.NewReader("{}"))
	r.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: admin})
	r.Header.Set("Origin", "https://attacker.invalid")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin proxy mutation", w.Code)
	}
}
