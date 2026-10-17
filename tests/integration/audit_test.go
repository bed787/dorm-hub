package integration

import (
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/locations"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditArchiveLimitEveryEntry(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "archive@students.example", false)
	ads := listings.New(db)
	draft := input()
	draft.Draft = true
	var first listings.Listing
	for i := 0; i < 50; i++ {
		l, err := ads.Create(ctx, owner, draft)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = l
		}
	}
	pending, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Transition(ctx, pending.ID, owner, "archive"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("archive action bypassed archive quota", err)
	}
	draft.Revision = pending.Revision
	if _, err = ads.Update(ctx, pending.ID, owner, draft); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("autosave bypassed archive quota", err)
	}
	draft.Revision = first.Revision
	if _, err = ads.Update(ctx, first.ID, owner, draft); err != nil {
		t.Fatal("full archive prevented updating an existing draft", err)
	}
	if err = ads.Delete(ctx, first.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Transition(ctx, pending.ID, owner, "archive"); err != nil {
		t.Fatal("deleting a draft did not free a slot", err)
	}
}

func TestAuditLongDormNumberDoesNotBreakCatalog(t *testing.T) {
	db := setup(t)
	service := locations.New(db)
	ctx := context.Background()
	for _, name := range []string{"№ 2", "№ 10", "№ " + strings.Repeat("9", 90)} {
		if _, err := service.CreateDorm(ctx, "NEVSKY", name); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := service.Catalog(ctx)
	if err != nil {
		t.Fatal("valid dorm name broke the shared catalog", err)
	}
	positions := map[string]int{}
	for i, d := range catalog.Dorms {
		positions[d.Name] = i
	}
	if positions["№ 2"] >= positions["№ 10"] || positions["№ 10"] >= positions["№ "+strings.Repeat("9", 90)] {
		t.Fatal("numeric ordering lost")
	}
}

func TestAuditExpiredAuthenticationCleanup(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "cleanup@students.example", false)
	login := auth.New(db, cfg(), &mailbox{})
	for _, email := range []string{"expired@students.example", "live@students.example"} {
		if err := login.Request(ctx, email); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE login_codes SET expires_at=now()-interval '2 hours' WHERE email='expired@students.example'`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES('expired',$1,'csrf',now()-interval '1 minute'),('live',$1,'csrf',now()+interval '1 hour')`,
		`INSERT INTO account_deletion_codes(user_id,session_hash,digest,expires_at) VALUES($1,'session','digest',now()-interval '2 hours')`,
	} {
		if _, err := db.Exec(ctx, query, owner); err != nil {
			t.Fatal(err)
		}
	}
	if err := login.PruneExpired(ctx); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT count(*) FROM login_codes WHERE email='expired@students.example'`,
		`SELECT count(*) FROM sessions WHERE token_hash='expired'`,
		`SELECT count(*) FROM account_deletion_codes`,
	} {
		var n int
		if err := db.QueryRow(ctx, query).Scan(&n); err != nil || n != 0 {
			t.Fatal("expired data remained", query, n, err)
		}
	}
	for _, query := range []string{
		`SELECT count(*) FROM login_codes WHERE email='live@students.example'`,
		`SELECT count(*) FROM sessions WHERE token_hash='live'`,
	} {
		var n int
		if err := db.QueryRow(ctx, query).Scan(&n); err != nil || n != 1 {
			t.Fatal("live data removed", query, n, err)
		}
	}
}

func TestAuditHTTPAuthorizationAndInjectionMatrix(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	attacker := account(t, db, "attacker@students.example", false)
	token, _ := auth.Token()
	if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, auth.Hash(token), attacker, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	ads := listings.New(db)
	private, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	handler := reviewAPI(t, db, &mailbox{})
	encoded, _ := json.Marshal(input())
	path := fmt.Sprintf("/api/listings/%d", private.ID)
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", path, "", 404},
		{"PUT", path, string(encoded), 404},
		{"DELETE", path, "", 404},
		{"POST", path + "/status", `{"action":"submit"}`, 404},
		{"GET", "/api/listings?scope=review", "", 403},
		{"GET", fmt.Sprintf("/api/listings?scope=admin_author&author_id=%d", owner), "", 403},
		{"GET", "/api/admin/users", "", 403},
		{"GET", "/api/admin/logs", "", 403},
		{"PUT", "/api/admin/site-settings", `{"maintenance":true,"reason":"attack"}`, 403},
		{"POST", fmt.Sprintf("/api/admin/listings/%d/review", private.ID), `{"action":"approve","revision":1}`, 403},
		{"PUT", "/api/me", `{"nickname":"attacker","is_admin":true}`, 400},
		{"GET", "/api/listings?q=%27+OR+1%3D1--", "", 200},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			w := reviewRequest(handler, test.method, test.path, test.body, token)
			if w.Code != test.status {
				t.Fatalf("status %d want %d: %s", w.Code, test.status, w.Body.String())
			}
		})
	}
	w := reviewRequest(handler, "GET", fmt.Sprintf("/api/users/%d", owner), "", token)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"email"`) || strings.Contains(w.Body.String(), "owner@students.example") {
		t.Fatal("profile leaks email", w.Body.String())
	}
	r := httptest.NewRequest("PUT", path, strings.NewReader(string(encoded)))
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", strings.Repeat("c", 64))
	r.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
	cross := httptest.NewRecorder()
	handler.ServeHTTP(cross, r)
	if cross.Code != 403 {
		t.Fatal("cross-site write accepted")
	}
	current, err := ads.Get(ctx, private.ID, owner, false)
	if err != nil || current.Status != "pending" || current.Revision != private.Revision {
		t.Fatal("attack changed private listing", err)
	}
}
