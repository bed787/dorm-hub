package integration

import (
	"bytes"
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/config"
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
	"dorm-hub/migrations"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var name string
	if err = db.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, "_test") {
		t.Fatal("integration database name must end with _test")
	}
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `TRUNCATE users,login_codes,dorms,reports,moderation_actions RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE site_statistics SET completed_deals=0; DELETE FROM pending_file_deletions`); err != nil {
		t.Fatal(err)
	}
	return db
}
func account(t *testing.T, db *pgxpool.Pool, email string, admin bool) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(context.Background(), `INSERT INTO users(email,nickname,is_admin,telegram) VALUES($1,$2,$3,'https://t.me/student') RETURNING id`, email, strings.Split(email, "@")[0], admin).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func input() listings.Input {
	return listings.Input{Kind: "offer", Category: "things", Title: "Настольная лампа", Description: "Лампа работает, можно забрать в кампусе.", Location: "PETRODVORTSOVY", PriceMode: "unset", Telegram: "https://t.me/student"}
}

type mailbox struct {
	mu   sync.Mutex
	code string
}

func (m *mailbox) SendCode(_ context.Context, _ string, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.code = code
	return nil
}
func (m *mailbox) SendDeletionCode(ctx context.Context, email, code string) error {
	return m.SendCode(ctx, email, code)
}
func cfg() config.Config {
	return config.Config{BaseURL: "http://localhost:8080", AllowedDomains: []string{"students.example"}, AdminEmails: []string{"admin@students.example"}, AuthSecret: strings.Repeat("a", 64)}
}
func TestOTPAttemptsAndSingleUse(t *testing.T) {
	db := setup(t)
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	ctx := context.Background()
	email := "alice@students.example"
	if err := login.Request(ctx, email); err != nil {
		t.Fatal(err)
	}
	if err := login.Request(ctx, email); !errors.Is(err, domain.ErrRateLimit) {
		t.Fatalf("cooldown not enforced: %v", err)
	}
	wrong := "000000"
	if mail.code == wrong {
		wrong = "000001"
	}
	for i := 0; i < 5; i++ {
		if _, _, err := login.Verify(ctx, email, wrong, false); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, _, err := login.Verify(ctx, email, mail.code, false); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("attempt limit bypassed")
	}
	if _, err := db.Exec(ctx, `UPDATE login_codes SET created_at=now()-interval '61 seconds'`); err != nil {
		t.Fatal(err)
	}
	if err := login.Request(ctx, email); err != nil {
		t.Fatal(err)
	}
	token, s, err := login.Verify(ctx, email, mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	if s.User.Nickname != "Студент_1" {
		t.Fatal(s.User.Nickname)
	}
	if _, _, err = login.Verify(ctx, email, mail.code, false); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("code reused")
	}
	if _, err = login.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err = login.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = login.Authenticate(ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("revoked session accepted")
	}
}

func TestExternalAdminEmailException(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	configuration := cfg()
	configuration.AllowedDomains = []string{"student.spbu.ru"}
	configuration.AllowedEmails = []string{"dormhub.connection@gmail.com"}
	configuration.AdminEmails = []string{"dormhub.connection@gmail.com"}
	mail := &mailbox{}
	login := auth.New(db, configuration, mail)
	for _, email := range []string{"other@gmail.com", "dormhub.connection+other@gmail.com", "dormhubconnection@gmail.com"} {
		if err := login.Request(ctx, email); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unauthorized address %q: %v", email, err)
		}
	}
	if mail.code != "" {
		t.Fatal("mail was sent to a disallowed address")
	}
	email := "dormhub.connection@gmail.com"
	if err := login.Request(ctx, email); err != nil {
		t.Fatal(err)
	}
	_, session, err := login.Verify(ctx, email, mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	if !session.User.IsAdmin {
		t.Fatal("external administrator role missing")
	}
	email = "st123456@student.spbu.ru"
	if err = login.Request(ctx, email); err != nil {
		t.Fatal(err)
	}
	_, session, err = login.Verify(ctx, email, mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	if session.User.IsAdmin {
		t.Fatal("student was granted admin rights")
	}
}

func TestDistrictMigrationPreservesListingsAndSeedsDorms(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	ids := map[string]int64{}
	for _, code := range []string{"PUNK", "VUNK", "DUNK"} {
		if _, err := db.Exec(ctx, `INSERT INTO locations(code,name) VALUES($1,$1)`, code); err != nil {
			t.Fatal(err)
		}
		var dorm, id int64
		if err := db.QueryRow(ctx, `INSERT INTO dorms(location,name) VALUES($1,'Старое общежитие') RETURNING id`, code).Scan(&dorm); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `INSERT INTO listings(author_id,kind,category,title,description,location,dorm_id,price_mode,telegram) VALUES($1,'offer','things','Настольная лампа','Рабочая лампа для комнаты.',$2,$3,'unset','https://t.me/student') RETURNING id`, owner, code, dorm).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[code] = id
	}
	data, err := os.ReadFile("../../migrations/002_districts.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Execute again to exercise migration on legacy data and idempotent seeding.
	for i := 0; i < 2; i++ {
		if _, err = db.Exec(ctx, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	ads := listings.New(db)
	for old, expected := range map[string]string{"PUNK": "PETRODVORTSOVY", "VUNK": "VASILEOSTROVSKY", "DUNK": "UNSPECIFIED"} {
		listing, err := ads.Get(ctx, ids[old], owner, false)
		if err != nil {
			t.Fatal(err)
		}
		if listing.Location != expected || listing.DormID == nil || listing.DormName != "Старое общежитие" {
			t.Fatalf("legacy listing not preserved: %+v", listing)
		}
	}
	catalog, err := locations.New(db).Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Locations) != 4 {
		t.Fatalf("districts: %+v", catalog.Locations)
	}
	counts := map[string]int{}
	for _, dorm := range catalog.Dorms {
		if strings.HasPrefix(dorm.Name, "№ ") {
			counts[dorm.Location]++
		}
	}
	if counts["VASILEOSTROVSKY"] != 8 || counts["PETRODVORTSOVY"] != 13 || counts["NEVSKY"] != 1 {
		t.Fatalf("dorm counts: %+v", counts)
	}
	// A dorm from a different district must not be accepted in a listing.
	var foreign int64
	if err = db.QueryRow(ctx, `SELECT id FROM dorms WHERE location='NEVSKY' LIMIT 1`).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Location = "VASILEOSTROVSKY"
	in.DormID = &foreign
	if _, err = ads.Create(ctx, owner, in); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("foreign district dorm accepted: %v", err)
	}
}
func TestConcurrentListingLimitAndLifecycle(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	viewer := account(t, db, "viewer@students.example", false)
	ads := listings.New(db)
	var successes atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ads.Create(ctx, owner, input())
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if successes.Load() != 10 {
		t.Fatalf("created %d listings, want 10", successes.Load())
	}
	page, err := ads.List(ctx, owner, listings.Filter{Scope: "mine", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	id := page.Items[0].ID
	publish(t, db, page.Items[0])
	if _, err = ads.Update(ctx, id, viewer, input()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign update accepted: %v", err)
	}
	if _, err = ads.Transition(ctx, id, owner, "complete"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE listings SET completed_at=now()-interval '25 hours' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Get(ctx, id, viewer, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("expired listing visible")
	}
	own, err := ads.Get(ctx, id, owner, false)
	if err != nil || own.Status != "completed" {
		t.Fatalf("archive state: %+v %v", own, err)
	}
	if _, err = ads.Transition(ctx, id, owner, "activate"); err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Create(ctx, owner, input()); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("reactivated listing not counted")
	}
}
func TestContactsNicknameStarsAndFavorites(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	a := account(t, db, "alice@students.example", false)
	b := account(t, db, "bob@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, a, input())
	if err != nil {
		t.Fatal(err)
	}
	publish(t, db, l)
	profiles := users.New(db)
	if _, err = profiles.Update(ctx, a, users.Update{Nickname: "Алиса", Telegram: "https://t.me/newcontact"}); err != nil {
		t.Fatal(err)
	}
	if _, err = profiles.Update(ctx, a, users.Update{Nickname: "Другой ник", Telegram: "https://t.me/newcontact"}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("second rename accepted")
	}
	l, err = ads.Get(ctx, l.ID, b, false)
	if err != nil || l.Telegram != "https://t.me/student" {
		t.Fatal("listing contacts changed with profile")
	}
	ratings := stars.New(db)
	for i := 0; i < 2; i++ {
		if err = ratings.Set(ctx, b, a, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = ratings.Set(ctx, a, a, true); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("self-star accepted")
	}
	u, err := profiles.Get(ctx, a, b)
	if err != nil || u.Stars != 1 {
		t.Fatal("duplicate star")
	}
	fav := favorites.New(db)
	if err = fav.Set(ctx, a, l.ID, true); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("own favorite accepted")
	}
	if err = fav.Set(ctx, b, l.ID, true); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultNicknameCannotBeClaimed(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	id := account(t, db, "first@students.example", false)
	profiles := users.New(db)
	if _, err := profiles.Update(ctx, id, users.Update{Nickname: "сТуДеНт_2"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("future default nickname could be claimed: %v", err)
	}
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	if err := login.Request(ctx, "second@students.example"); err != nil {
		t.Fatal(err)
	}
	_, session, err := login.Verify(ctx, "second@students.example", mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	if session.User.Nickname != "Студент_2" {
		t.Fatalf("default nickname: %q", session.User.Nickname)
	}
	// Keeping one's own default nickname while updating contacts is allowed.
	if _, err := profiles.Update(ctx, session.User.ID, users.Update{Nickname: session.User.Nickname, Telegram: "https://t.me/student"}); err != nil {
		t.Fatal(err)
	}
}
func TestPhotosPrivateAndLimited(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	a := account(t, db, "alice@students.example", false)
	b := account(t, db, "bob@students.example", false)
	l, err := listings.New(db).Create(ctx, a, input())
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.New(db, files)
	var buffer bytes.Buffer
	if err = jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 100, 60)), nil); err != nil {
		t.Fatal(err)
	}
	var first string
	for i := 0; i < 10; i++ {
		p, err := photos.Upload(ctx, l.ID, a, buffer.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p.ID
		}
	}
	if _, err = photos.Upload(ctx, l.ID, a, buffer.Bytes()); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("photo limit bypassed")
	}
	if err = photos.Delete(ctx, first, l.ID, b); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("foreign photo deleted")
	}
	if _, err = listings.New(db).Transition(ctx, l.ID, a, "archive"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = photos.Open(ctx, first, b, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("archived photo exposed")
	}
}
func TestModerationAndAuthBoundaries(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	viewer := account(t, db, "viewer@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	mod := moderation.New(db)
	if err = mod.Action(ctx, viewer, "user", owner, "block", "Попытка без административных прав"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("non-admin business operation accepted")
	}
	if err = mod.Action(ctx, admin, "user", owner, "block", "Нарушение правил площадки"); err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Get(ctx, l.ID, viewer, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("blocked author's listing visible")
	}
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	if err = login.Request(ctx, "viewer@students.example"); err != nil {
		t.Fatal(err)
	}
	token, session, err := login.Verify(ctx, "viewer@students.example", mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(cfg(), slog.New(slog.NewTextHandler(io.Discard, nil)), db, login, users.New(db), ads, locations.New(db), favorites.New(db), stars.New(db), mod, media.New(db, files))
	request := httptest.NewRequest("GET", "/api/listings", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal("anonymous feed accessible")
	}
	for _, test := range []struct {
		path, method, origin, csrf string
		status                     int
	}{{"/api/admin/users", "GET", "", "", 403}, {"/api/me", "PUT", "http://evil.test", session.CSRF, 403}, {"/api/me", "DELETE", cfg().BaseURL, "wrong", 403}} {
		request = httptest.NewRequest(test.method, test.path, nil)
		request.Header.Set("Origin", test.origin)
		request.Header.Set("X-CSRF-Token", test.csrf)
		request.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s: got %d", test.path, response.Code)
		}
	}
}

func TestConcurrentCreateRetriesReturnOneListing(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "retry@students.example", false)
	ads := listings.New(db)
	key := strings.Repeat("a", 64)
	ids := make(chan int64, 20)
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := ads.CreateOnce(ctx, owner, input(), key)
			if err != nil {
				errs <- err
			} else {
				ids <- l.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var expected int64
	for id := range ids {
		if expected == 0 {
			expected = id
		}
		if id != expected {
			t.Fatal("retry created another listing")
		}
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM listings WHERE author_id=$1`, owner).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate creations", count, err)
	}
	changed := input()
	changed.Title = "Другое название"
	if _, err := ads.CreateOnce(ctx, owner, changed, key); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("key reused with different input", err)
	}
	for i := 0; i < 9; i++ {
		if _, err := ads.Create(ctx, owner, input()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ads.CreateOnce(ctx, owner, input(), key); err != nil {
		t.Fatal("retry incorrectly consumed the active listing limit", err)
	}
}

func TestPhotoRetriesQuotaAndLegacySizes(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "photos@students.example", false)
	ads := listings.New(db)
	first, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err = jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 100, 60)), nil); err != nil {
		t.Fatal(err)
	}
	normalized, _, _, err := media.Process(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.NewWithLimits(db, files, int64(len(normalized)), 10)
	key := strings.Repeat("b", 64)
	p, err := photos.UploadOnce(ctx, first.ID, owner, buffer.Bytes(), key)
	if err != nil {
		t.Fatal(err)
	}
	again, err := photos.UploadOnce(ctx, first.ID, owner, buffer.Bytes(), key)
	if err != nil || again.ID != p.ID {
		t.Fatal("photo retry did not recover its result", err)
	}
	if _, err = photos.UploadOnce(ctx, first.ID, owner, append(buffer.Bytes(), 0), key); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("photo retry accepted changed bytes", err)
	}
	if _, err = ads.Transition(ctx, first.ID, owner, "archive"); err != nil {
		t.Fatal(err)
	}
	if _, err = photos.Upload(ctx, second.ID, owner, buffer.Bytes()); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("archiving bypassed account quota", err)
	}
	if _, err = db.Exec(ctx, `UPDATE photos SET size_bytes=0 WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = photos.BackfillSizes(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	var size int64
	if err = db.QueryRow(ctx, `SELECT size_bytes FROM photos WHERE id=$1`, p.ID).Scan(&size); err != nil || size != int64(len(normalized)) {
		t.Fatal("legacy file was not measured", size, err)
	}
	if err = photos.Delete(ctx, p.ID, first.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = photos.UploadOnce(ctx, first.ID, owner, buffer.Bytes(), key); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("retry resurrected a deleted photo", err)
	}
	if _, err = photos.Upload(ctx, second.ID, owner, buffer.Bytes()); err != nil {
		t.Fatal("deleting a photo did not free its quota", err)
	}
}

func TestConcurrentPhotoQuotaAcrossListings(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "quota@students.example", false)
	ads := listings.New(db)
	list := []int64{}
	for i := 0; i < 4; i++ {
		l, err := ads.Create(ctx, owner, input())
		if err != nil {
			t.Fatal(err)
		}
		list = append(list, l.ID)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.NewWithLimits(db, files, 1024*1024, 2)
	var buffer bytes.Buffer
	if err = jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for _, id := range list {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_, err := photos.Upload(ctx, id, owner, buffer.Bytes())
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				errs <- err
			}
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if successes.Load() != 2 {
		t.Fatalf("quota accepted %d photos, expected 2", successes.Load())
	}
}

func TestAdminCanInspectBlockedProfileAndListings(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "blocked@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	viewer := account(t, db, "viewer@students.example", false)
	ads := listings.New(db)
	profiles := users.New(db)
	mod := moderation.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Transition(ctx, l.ID, owner, "archive"); err != nil {
		t.Fatal(err)
	}
	if err = mod.Action(ctx, admin, "listing", l.ID, "hide", "Скрыто для проверки модератором"); err != nil {
		t.Fatal(err)
	}
	if err = mod.Action(ctx, admin, "user", owner, "block", "Заблокирован для проверки модератором"); err != nil {
		t.Fatal(err)
	}
	if _, err = profiles.Get(ctx, owner, viewer); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("blocked profile visible to a student", err)
	}
	user, err := profiles.GetForViewer(ctx, owner, admin, true)
	if err != nil || !user.Blocked {
		t.Fatal("admin cannot inspect blocked profile", err)
	}
	f := listings.Filter{Scope: "admin_author", AuthorID: owner, Limit: 24}
	if _, err = ads.ListForViewer(ctx, viewer, f, false); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("student accessed moderator listing scope", err)
	}
	page, err := ads.ListForViewer(ctx, admin, f, true)
	if err != nil || page.Total != 1 {
		t.Fatal("admin cannot inspect hidden archived listing", err)
	}
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	if err = login.Request(ctx, "admin@students.example"); err != nil {
		t.Fatal(err)
	}
	token, _, err := login.Verify(ctx, "admin@students.example", mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(cfg(), slog.New(slog.NewTextHandler(io.Discard, nil)), db, login, profiles, ads, locations.New(db), favorites.New(db), stars.New(db), mod, media.New(db, files))
	request := httptest.NewRequest("GET", "/api/users/"+strconv.FormatInt(owner, 10), nil)
	request.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("HTTP admin profile access failed", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "@students.example") {
		t.Fatal("profile response exposed email")
	}
	path := "/api/listings?scope=admin_author&author_id=" + strconv.FormatInt(owner, 10)
	request = httptest.NewRequest("GET", path, nil)
	request.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: token})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("HTTP admin listing access failed", response.Code)
	}
	if err = login.Request(ctx, "viewer@students.example"); err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := login.Verify(ctx, "viewer@students.example", mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest("GET", path, nil)
	request.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: viewerToken})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("HTTP student accessed moderator listings", response.Code)
	}
	request = httptest.NewRequest("GET", "/api/users/"+strconv.FormatInt(owner, 10), nil)
	request.AddCookie(&http.Cookie{Name: cfg().SessionCookie(), Value: viewerToken})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatal("HTTP student accessed blocked profile", response.Code)
	}
}

func publish(t *testing.T, db *pgxpool.Pool, l listings.Listing) listings.Listing {
	t.Helper()
	ctx := context.Background()
	var admin int64
	if err := db.QueryRow(ctx, `SELECT id FROM users WHERE is_admin LIMIT 1`).Scan(&admin); err != nil {
		admin = account(t, db, "admin@students.example", true)
	}
	latest, err := listings.New(db).Get(ctx, l.ID, l.AuthorID, false)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := listings.New(db).Review(ctx, l.ID, admin, latest.Revision, true, "")
	if err != nil {
		t.Fatal(err)
	}
	return approved
}
func TestDraftReviewPrivacyAndContacts(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	viewer := account(t, db, "viewer@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	if _, err := db.Exec(ctx, `UPDATE users SET telegram='',vk='' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	ads := listings.New(db)
	draft := listings.Input{Draft: true, Kind: "offer", Title: "Л", PriceMode: "unset"}
	l, err := ads.Create(ctx, owner, draft)
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != "archived" || l.ArchiveKind != "draft" {
		t.Fatal(l)
	}
	if _, err = ads.Get(ctx, l.ID, viewer, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("draft exposed", err)
	}
	if _, err = ads.TransitionWithOutcome(ctx, l.ID, owner, "submit", ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("incomplete draft submitted", err)
	}
	in := input()
	in.Draft = true
	in.Revision = l.Revision
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	l, err = ads.TransitionWithOutcome(ctx, l.ID, owner, "submit", "")
	if err != nil {
		t.Fatal("listing contacts were rejected with a blank profile", err)
	}
	if l.Status != "pending" {
		t.Fatal(l.Status)
	}
	if _, err = ads.Get(ctx, l.ID, viewer, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("pending entry exposed", err)
	}
	if _, err = ads.Review(ctx, l.ID, viewer, l.Revision, true, ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("student approved", err)
	}
	if _, err = ads.Review(ctx, l.ID, admin, l.Revision, false, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("empty rejection comment", err)
	}
	l, err = ads.Review(ctx, l.ID, admin, l.Revision, false, "Добавьте подробности состояния")
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != "archived" || l.ArchiveKind != "rejected" || l.ModerationComment == "" {
		t.Fatal(l)
	}
	l, err = ads.TransitionWithOutcome(ctx, l.ID, owner, "submit", "")
	if err != nil {
		t.Fatal(err)
	}
	oldRevision := l.Revision
	in = input()
	in.Title = "Обновлённая лампа"
	in.Revision = l.Revision
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ads.Review(ctx, l.ID, admin, oldRevision, true, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale review accepted", err)
	}
	l = publish(t, db, l)
	if _, err = ads.Get(ctx, l.ID, viewer, false); err != nil {
		t.Fatal("approved not visible", err)
	}
	in = input()
	in.Title = "Изменено после публикации"
	in.Revision = l.Revision
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != "pending" {
		t.Fatal("edit bypassed moderation", l.Status)
	}
	if _, err = ads.Get(ctx, l.ID, viewer, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("edited entry exposed", err)
	}
}
func TestPhotoEditInvalidatesReview(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	viewer := account(t, db, "viewer@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	l = publish(t, db, l)
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.New(db, files)
	var buffer bytes.Buffer
	if err = jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil); err != nil {
		t.Fatal(err)
	}
	p, err := photos.Upload(ctx, l.ID, owner, buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ads.Get(ctx, l.ID, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Status != "pending" || changed.Revision <= l.Revision {
		t.Fatal("photo did not invalidate publication")
	}
	if _, _, err = photos.Open(ctx, p.ID, viewer, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("pending photo exposed", err)
	}
	if _, err = ads.Review(ctx, l.ID, accountAdmin(t, db), l.Revision, true, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("old snapshot approved", err)
	}
	changed = publish(t, db, changed)
	file, _, err := photos.Open(ctx, p.ID, viewer, false)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err = photos.Delete(ctx, p.ID, l.ID, owner); err != nil {
		t.Fatal(err)
	}
	changed, err = ads.Get(ctx, l.ID, owner, false)
	if err != nil || changed.Status != "pending" {
		t.Fatal("photo delete bypassed review", err)
	}
}
func accountAdmin(t *testing.T, db *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(context.Background(), `SELECT id FROM users WHERE is_admin LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestCompletionCounterSurvivesDeletion(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "owner@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	l = publish(t, db, l)
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ads.TransitionWithOutcome(ctx, l.ID, owner, "complete", "success")
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	total, err := ads.CompletedDeals(ctx)
	if err != nil || total != 1 || successes.Load() != 1 {
		t.Fatal(total, successes.Load(), err)
	}
	l, err = ads.Get(ctx, l.ID, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Draft = true
	in.Revision = l.Revision
	l, err = ads.Update(ctx, l.ID, owner, in)
	if err != nil || l.Status != "completed" {
		t.Fatal("completed edit republished", err)
	}
	l, err = ads.TransitionWithOutcome(ctx, l.ID, owner, "submit", "")
	if err != nil {
		t.Fatal(err)
	}
	l = publish(t, db, l)
	if _, err = ads.TransitionWithOutcome(ctx, l.ID, owner, "complete", "success"); err != nil {
		t.Fatal(err)
	}
	other, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	other = publish(t, db, other)
	if _, err = ads.TransitionWithOutcome(ctx, other.ID, owner, "complete", "no_longer_needed"); err != nil {
		t.Fatal(err)
	}
	total, _ = ads.CompletedDeals(ctx)
	if total != 1 {
		t.Fatal("counter counted twice or counted cancellation", total)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, owner); err != nil {
		t.Fatal(err)
	}
	if err = users.DeleteInTx(ctx, tx, owner, "owner@students.example"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	total, _ = ads.CompletedDeals(ctx)
	if total != 1 {
		t.Fatal("aggregate removed with account")
	}
}
func TestAccountDeletionIsCompleteAndRequiresFreshCode(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	email := "owner@students.example"
	if err := login.Request(ctx, email); err != nil {
		t.Fatal(err)
	}
	loginCode := mail.code
	token, session, err := login.Verify(ctx, email, loginCode, false)
	if err != nil {
		t.Fatal(err)
	}
	owner := session.User.ID
	other := account(t, db, "viewer@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	l = publish(t, db, l)
	if _, err = moderation.New(db).Report(ctx, other, "listing", l.ID, "Подробная жалоба на автора"); err != nil {
		t.Fatal(err)
	}
	if _, err = moderation.New(db).Report(ctx, owner, "user", other, "Жалоба владельца аккаунта"); err != nil {
		t.Fatal(err)
	}
	if err = stars.New(db).Set(ctx, other, owner, true); err != nil {
		t.Fatal(err)
	}
	if err = favorites.New(db).Set(ctx, other, l.ID, true); err != nil {
		t.Fatal(err)
	}
	files, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	photos := media.New(db, files)
	var buffer bytes.Buffer
	if err = jpeg.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil); err != nil {
		t.Fatal(err)
	}
	p, err := photos.Upload(ctx, l.ID, owner, buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = login.DeleteAccount(ctx, owner, token, loginCode); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("login code deleted account", err)
	}
	if err = login.RequestDeletion(ctx, owner, token); err != nil {
		t.Fatal(err)
	}
	deletionCode := mail.code
	if err = login.RequestDeletion(ctx, owner, token); !errors.Is(err, domain.ErrRateLimit) {
		t.Fatal("deletion resend cooldown", err)
	}
	if err = login.DeleteAccount(ctx, owner, strings.Repeat("z", 64), deletionCode); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("foreign session used code", err)
	}
	wrong := "000000"
	if wrong == deletionCode {
		wrong = "000001"
	}
	for i := 0; i < 5; i++ {
		if err = login.DeleteAccount(ctx, owner, token, wrong); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if err = login.DeleteAccount(ctx, owner, token, deletionCode); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("attempt limit ignored", err)
	}
	if _, err = db.Exec(ctx, `UPDATE account_deletion_codes SET created_at=now()-interval '61 seconds'`); err != nil {
		t.Fatal(err)
	}
	if err = login.RequestDeletion(ctx, owner, token); err != nil {
		t.Fatal(err)
	}
	deletionCode = mail.code
	if _, err = db.Exec(ctx, `UPDATE account_deletion_codes SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if err = login.DeleteAccount(ctx, owner, token, deletionCode); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("expired code accepted", err)
	}
	if _, err = db.Exec(ctx, `UPDATE account_deletion_codes SET created_at=now()-interval '61 seconds'`); err != nil {
		t.Fatal(err)
	}
	if err = login.RequestDeletion(ctx, owner, token); err != nil {
		t.Fatal(err)
	}
	if err = login.DeleteAccount(ctx, owner, token, mail.code); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sessions", "listings", "photos", "favorites", "stars", "reports", "moderation_actions", "idempotency_keys", "account_deletion_codes", "login_codes"} {
		var count int
		if err = db.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("remaining data in", table, count, err)
		}
	}
	if _, err = login.Authenticate(ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("session survived", err)
	}
	if err = photos.FlushDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = files.Open(p.ID + ".jpg"); !os.IsNotExist(err) {
		t.Fatal("file survived", err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=$1`, owner).Scan(&count); err != nil || count != 0 {
		t.Fatal("user survived")
	}
	if err = login.Request(ctx, email); err != nil {
		t.Fatal(err)
	}
	_, fresh, err := login.Verify(ctx, email, mail.code, false)
	if err != nil || fresh.User.ID == owner {
		t.Fatal("registration restored old account", err)
	}
	_ = admin
}
