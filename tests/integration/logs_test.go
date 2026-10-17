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
	"dorm-hub/internal/platform/logview"
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

func TestAdministratorApplicationLogs(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	_, _ = db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`)
	t.Cleanup(func() { _, _ = db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`) })
	admin := account(t, db, "admin@students.example", true)
	member := account(t, db, "member@students.example", false)
	session := func(id int64) string {
		token, _ := auth.Token()
		_, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, auth.Hash(token), id, strings.Repeat("c", 64))
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	adminToken, memberToken := session(admin), session(member)
	logs := logview.New()
	logger := slog.New(logview.NewHandler(slog.NewTextHandler(io.Discard, nil), logs))
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(cfg(), logger, db, auth.New(db, cfg(), &mailbox{}), users.New(db), listings.New(db), locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), media.New(db, files), logs)
	check := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status %d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	check(reviewRequest(handler, "GET", "/api/admin/logs", "", ""), http.StatusUnauthorized)
	check(reviewRequest(handler, "GET", "/api/admin/logs", "", memberToken), http.StatusForbidden)
	logger.Error("database failed", "request_id", "lookup-42", "email", "hidden@example.test", "token", "do-not-expose")
	w := reviewRequest(handler, "GET", "/api/admin/logs?level=ERROR&q=lookup-42", "", adminToken)
	check(w, 200)
	if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("log response can be cached")
	}
	var body struct {
		Data logview.Snapshot `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].Message != "database failed" {
		t.Fatal("log query failed", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "hidden@example") || strings.Contains(w.Body.String(), "do-not-expose") {
		t.Fatal("private data leaked")
	}
	before := logs.Read("", "").Retained
	for i := 0; i < 3; i++ {
		check(reviewRequest(handler, "GET", "/api/admin/logs", "", adminToken), 200)
	}
	if logs.Read("", "").Retained != before {
		t.Fatal("log viewer logs itself")
	}
	check(reviewRequest(handler, "GET", "/api/admin/logs?level=INVALID", "", adminToken), 400)
	if _, err := db.Exec(ctx, `UPDATE site_settings SET maintenance=true,maintenance_reason='Обновление'`); err != nil {
		t.Fatal(err)
	}
	check(reviewRequest(handler, "GET", "/api/admin/logs", "", adminToken), 200)
	if _, err := db.Exec(ctx, `UPDATE users SET is_admin=false WHERE id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	check(reviewRequest(handler, "GET", "/api/admin/logs", "", adminToken), 503)
	if _, err := db.Exec(ctx, `UPDATE site_settings SET maintenance=false,maintenance_reason=''`); err != nil {
		t.Fatal(err)
	}
	check(reviewRequest(handler, "GET", "/api/admin/logs", "", adminToken), 403)
}
