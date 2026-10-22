package integration

import (
	"bytes"
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/locations"
	"dorm-hub/internal/media"
	"dorm-hub/internal/platform/filestore"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestExternalLocationAddressLifecycle(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "external@students.example", false)
	viewer := account(t, db, "viewer@students.example", false)
	ads := listings.New(db)
	in := input()
	in.Location = "EXTERNAL"
	// The address is optional, including when submitting for review.
	l, err := ads.Create(ctx, owner, in)
	if err != nil || l.Address != "" {
		t.Fatal(l, err)
	}
	in.Address = "  Санкт-Петербург, улица Примерная, 10  "
	in.Revision = l.Revision
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil || l.Address != strings.TrimSpace(in.Address) {
		t.Fatal(l, err)
	}
	l = publish(t, db, l)
	page, err := ads.List(ctx, viewer, listings.Filter{Location: "EXTERNAL"})
	if err != nil || page.Total != 1 || page.Items[0].Address != l.Address {
		t.Fatal(page, err)
	}
	catalog, err := locations.New(db).Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, loc := range catalog.Locations {
		if loc.Code == "EXTERNAL" && loc.Name == "Не УНК университета" {
			found = true
		}
	}
	if !found {
		t.Fatal("external location missing from catalog")
	}
	if _, err = locations.New(db).CreateDorm(ctx, "EXTERNAL", "Чужой адрес"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("external address entered the dorm directory", err)
	}
	in.Revision = l.Revision
	in.Draft = true
	in.Address = "Другой адрес"
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil || l.Status != "archived" || l.Address != in.Address {
		t.Fatal(l, err)
	}
	l, err = ads.Transition(ctx, l.ID, owner, "submit")
	if err != nil || l.Status != "pending" || l.Address != in.Address {
		t.Fatal(l, err)
	}
	in.Revision = l.Revision
	in.Location = "NEVSKY"
	in.Address = ""
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil || l.Address != "" || l.Location != "NEVSKY" {
		t.Fatal(l, err)
	}
	if _, err = db.Exec(ctx, `UPDATE listings SET address='Не тот адрес' WHERE id=$1`, l.ID); err == nil {
		t.Fatal("database accepted address for an internal location")
	}
}

func TestSavedPhotoCropIsAtomicAtFullLimitsAndRetryable(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "crop@students.example", false)
	other := account(t, db, "other@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := jpegData(t)
	normalized, _, _, err := media.Process(data)
	if err != nil {
		t.Fatal(err)
	}
	photos := media.NewWithLimits(db, files, int64(len(normalized)*10), 10)
	var original listings.Photo
	for i := 0; i < 10; i++ {
		p, e := photos.Upload(ctx, l.ID, owner, data)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			original = p
		}
	}
	l, err = ads.Get(ctx, l.ID, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	l = publish(t, db, l)
	read := func() ([]byte, error) { return data, nil }
	if _, err = photos.ReplaceFrom(ctx, l.ID, other, original.ID, "foreign-crop-key-0001", read); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("foreign crop accepted", err)
	}
	cropped, err := photos.ReplaceFrom(ctx, l.ID, owner, original.ID, "saved-crop-key-00001", read)
	if err != nil || cropped.ID == original.ID || cropped.Position != 0 {
		t.Fatal(cropped, err)
	}
	changed, err := ads.Get(ctx, l.ID, owner, false)
	if err != nil || len(changed.Photos) != 10 || changed.Photos[0].ID != cropped.ID || changed.Status != "pending" || changed.Revision != l.Revision+1 {
		t.Fatal(changed, err)
	}
	if _, _, err = photos.Open(ctx, original.ID, owner, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("replaced photo still accessible", err)
	}
	if _, err = files.Open(original.ID + ".jpg"); !os.IsNotExist(err) {
		t.Fatal("old file was not cleaned up", err)
	}
	file, _, err := photos.Open(ctx, cropped.ID, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	retry, err := photos.ReplaceFrom(ctx, l.ID, owner, original.ID, "saved-crop-key-00001", read)
	if err != nil || retry.ID != cropped.ID {
		t.Fatal("uncertain response produced another crop", retry, err)
	}
	after, err := ads.Get(ctx, l.ID, owner, false)
	if err != nil || after.Revision != changed.Revision || len(after.Photos) != 10 {
		t.Fatal(after, err)
	}
	if _, err = photos.ReplaceFrom(ctx, l.ID, owner, original.ID, "saved-crop-key-00001", func() ([]byte, error) { return append(append([]byte{}, data...), 1), nil }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("retry key accepted a different crop", err)
	}
	if _, err = photos.Upload(ctx, l.ID, owner, data); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("new photo bypassed ten-photo limit", err)
	}
}

func TestFailedSavedPhotoCropPreservesOriginal(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "safe-crop@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.New(db, files)
	data := jpegData(t)
	original, err := photos.Upload(ctx, l.ID, owner, data)
	if err != nil {
		t.Fatal(err)
	}
	l, err = ads.Get(ctx, l.ID, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	// Invalid image and insufficient byte quota must leave both DB and file intact.
	if _, err = photos.ReplaceFrom(ctx, l.ID, owner, original.ID, "invalid-crop-key-001", func() ([]byte, error) { return []byte("not an image"), nil }); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	restricted := media.NewWithLimits(db, files, 1, 1)
	if _, err = restricted.ReplaceFrom(ctx, l.ID, owner, original.ID, "quota-crop-key-00001", func() ([]byte, error) { return data, nil }); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("oversized replacement accepted", err)
	}
	after, err := ads.Get(ctx, l.ID, owner, false)
	if err != nil || after.Revision != l.Revision || len(after.Photos) != 1 || after.Photos[0].ID != original.ID {
		t.Fatal(after, err)
	}
	file, _, err := photos.Open(ctx, original.ID, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM pending_file_deletions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed crop queued the original for deletion", count, err)
	}
	// After a definite failure the same photo can still be replaced normally.
	if _, err = photos.ReplaceFrom(ctx, l.ID, owner, original.ID, "valid-crop-key-00001", func() ([]byte, error) { return bytes.Clone(data), nil }); err != nil {
		t.Fatal(err)
	}
}

func TestPhotoCropHTTPAuthorizationAndCSRF(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	if err := login.Request(ctx, "http-crop@students.example"); err != nil {
		t.Fatal(err)
	}
	token, session, err := login.Verify(ctx, "http-crop@students.example", mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	l, err := listings.New(db).Create(ctx, session.User.ID, input())
	if err != nil {
		t.Fatal(err)
	}
	handler := reviewAPI(t, db, mail)
	send := func(method, path, key, csrf, cookie string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, e := writer.CreateFormFile("file", "photo.jpg")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = part.Write(jpegData(t)); e != nil {
			t.Fatal(e)
		}
		if e = writer.Close(); e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(method, path, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Origin", cfg().BaseURL)
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("X-CSRF-Token", csrf)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: cookie})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	path := "/api/listings/" + strconv.FormatInt(l.ID, 10) + "/photos"
	w := send("POST", path, "http-upload-key-0001", session.CSRF, token)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var uploaded struct{ Data listings.Photo }
	if err = json.Unmarshal(w.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	path += "/" + uploaded.Data.ID
	if w = send("PUT", path, "http-crop-key-000001", "", token); w.Code != 403 {
		t.Fatal("crop accepted without CSRF", w.Code, w.Body.String())
	}
	if w = send("PUT", path, "http-crop-key-000001", session.CSRF, ""); w.Code != 401 {
		t.Fatal("guest crop accepted", w.Code, w.Body.String())
	}
	w = send("PUT", path, "http-crop-key-000001", session.CSRF, token)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var changed struct{ Data listings.Photo }
	if err = json.Unmarshal(w.Body.Bytes(), &changed); err != nil || changed.Data.ID == uploaded.Data.ID || changed.Data.Position != 0 {
		t.Fatal(changed, err)
	}
	// Replaying the exact HTTP request returns that same replacement.
	w = send("PUT", path, "http-crop-key-000001", session.CSRF, token)
	var retry struct{ Data listings.Photo }
	if err = json.Unmarshal(w.Body.Bytes(), &retry); err != nil || w.Code != 200 || retry.Data.ID != changed.Data.ID {
		t.Fatal(w.Code, w.Body.String(), err)
	}
}
