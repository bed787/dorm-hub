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
	"strconv"
	"strings"
	"testing"
)

func TestAdminAccountsEmailPrivacy(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	const adminEmail = "moderator@students.example"
	const studentEmail = "student+tag@students.example"
	adminID := account(t, db, adminEmail, true)
	studentID := account(t, db, studentEmail, false)
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	tokenFor := func(email string) string {
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
	adminToken := tokenFor(adminEmail)
	studentToken := tokenFor(studentEmail)
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(cfg(), slog.New(slog.NewTextHandler(io.Discard, nil)), db, login, users.New(db), listings.New(db), locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), media.New(db, files))
	get := func(path, token string, status int) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			request.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("GET %s: got %d, want %d: %s", path, response.Code, status, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET %s is cacheable", path)
		}
		return response
	}
	response := get("/api/admin/users", adminToken, http.StatusOK)
	var payload struct {
		Data []struct {
			ID       int64  `json:"id"`
			Nickname string `json:"nickname"`
			Email    string `json:"email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{adminID: adminEmail, studentID: studentEmail}
	if len(payload.Data) != len(want) {
		t.Fatalf("unexpected account count: %d", len(payload.Data))
	}
	for _, user := range payload.Data {
		if expected, ok := want[user.ID]; !ok || user.Email != expected || user.Nickname == "" {
			t.Fatalf("wrong account email or nickname: %+v", user)
		}
	}
	for _, c := range []struct {
		token  string
		status int
	}{{"", http.StatusUnauthorized}, {studentToken, http.StatusForbidden}} {
		denied := get("/api/admin/users", c.token, c.status)
		if strings.Contains(denied.Body.String(), studentEmail) || strings.Contains(denied.Body.String(), adminEmail) {
			t.Fatal("denied account list exposed email")
		}
	}
	profile := get("/api/users/"+strconv.FormatInt(adminID, 10), studentToken, http.StatusOK)
	if strings.Contains(profile.Body.String(), adminEmail) || strings.Contains(profile.Body.String(), `"email"`) {
		t.Fatal("public profile exposed email")
	}
	if _, err := db.Exec(ctx, `UPDATE users SET is_admin=false WHERE id=$1`, adminID); err != nil {
		t.Fatal(err)
	}
	get("/api/admin/users", adminToken, http.StatusForbidden)
}
