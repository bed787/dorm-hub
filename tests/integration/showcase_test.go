package integration

import (
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/locations"
	"dorm-hub/migrations"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestShowcaseCountsVisibilityLocationsAndModeration(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	ads := listings.New(db)
	a := account(t, db, "alice@students.example", false)
	b := account(t, db, "bob@students.example", false)
	admin := account(t, db, "admin@students.example", true)
	blocked := account(t, db, "blocked@students.example", false)
	var dorm12, dorm13, dorm11 int64
	for i, number := range []string{"12", "13", "11"} {
		loc := "PETRODVORTSOVY"
		if number == "11" {
			loc = "GSOM"
		}
		var id int64
		if err := db.QueryRow(ctx, `INSERT INTO dorms(location,name) VALUES($1,$2) RETURNING id`, loc, "№ "+number).Scan(&id); err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0:
			dorm12 = id
		case 1:
			dorm13 = id
		case 2:
			dorm11 = id
		}
	}
	makeAd := func(owner int64, cat, kind string, dorm int64, public bool) listings.Listing {
		t.Helper()
		in := input()
		in.Category = cat
		in.Kind = kind
		in.Location = ""
		in.DormID = &dorm
		l, err := ads.Create(ctx, owner, in)
		if err != nil {
			t.Fatal(err)
		}
		if public {
			l = publish(t, db, l)
		}
		return l
	}
	first := makeAd(a, "clothes", "offer", dorm12, true)
	makeAd(a, "clothes", "offer", dorm12, true)
	makeAd(b, "clothes", "offer", dorm13, true)
	tech := makeAd(b, "things", "offer", dorm13, true)
	makeAd(b, "clothes", "offer", dorm13, false)
	makeAd(b, "things", "request", dorm13, true)
	makeAd(a, "things", "offer", dorm11, true)
	for i := 0; i < 6; i++ {
		makeAd(a, "other", "offer", dorm12, true)
	}
	makeAd(blocked, "clothes", "offer", dorm12, true)
	if _, err := db.Exec(ctx, `UPDATE users SET blocked=true WHERE id=$1`, blocked); err != nil {
		t.Fatal(err)
	}
	f := listings.Filter{Location: "PETRODVORTSOVY", Kind: "offer"}
	page, err := ads.Showcase(ctx, a, f, "123")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 10 || page.KindTotals["request"] != 1 || len(page.Sections) != 3 || page.Sections[0].Code != "clothes" || page.Sections[0].Total != 3 || page.Sections[2].Code != "other" || page.Sections[2].Total != 6 {
		t.Fatalf("wrong counts/order: %+v", page)
	}
	for _, section := range page.Sections {
		for _, l := range section.Items {
			if l.Location != "PETRODVORTSOVY" || l.Status != "active" || l.Description != "" || l.Telegram != "" {
				t.Fatalf("invalid preview: %+v", l)
			}
		}
	}
	again, err := ads.Showcase(ctx, a, f, "123")
	if err != nil || !reflect.DeepEqual(page, again) {
		t.Fatal("unstable overview", err)
	}
	if _, err = ads.Showcase(ctx, a, f, "1;DROP TABLE users"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("invalid seed accepted", err)
	}
	if _, err = ads.SetCategory(ctx, first.ID, b, first.Revision, "home"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("member changed category", err)
	}
	changed, err := ads.SetCategory(ctx, first.ID, admin, first.Revision, "home")
	if err != nil || changed.Status != "active" || changed.AuthorID != a || changed.Revision != first.Revision+1 {
		t.Fatal(changed, err)
	}
	if _, err = ads.SetCategory(ctx, first.ID, admin, first.Revision, "help"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale classification accepted", err)
	}
	var queued bool
	if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM search_index_queue WHERE listing_id=$1)`, first.ID).Scan(&queued); err != nil || !queued {
		t.Fatal("classification not enqueued for search", err)
	}
	// Category correction must not turn a pending/hidden ad into a publication.
	pending := makeAd(b, "other", "offer", dorm13, false)
	pending, err = ads.SetCategory(ctx, pending.ID, admin, pending.Revision, "accessories")
	if err != nil || pending.Status != "pending" {
		t.Fatal(pending, err)
	}
	if _, err = db.Exec(ctx, `UPDATE listings SET hidden=true WHERE id=$1`, tech.ID); err != nil {
		t.Fatal(err)
	}
	page, err = ads.Showcase(ctx, a, f, "123")
	if err != nil || page.Total != 9 {
		t.Fatal("hidden entry in overview", page, err)
	}
	mail := &mailbox{}
	login := auth.New(db, cfg(), mail)
	if err = login.Request(ctx, "alice@students.example"); err != nil {
		t.Fatal(err)
	}
	token, _, err := login.Verify(ctx, "alice@students.example", mail.code, false)
	if err != nil {
		t.Fatal(err)
	}
	handler := reviewAPI(t, db, mail)
	if w := reviewRequest(handler, "GET", "/api/showcase", "", ""); w.Code != 401 {
		t.Fatal("public overview leaks ads", w.Code)
	}
	if w := reviewRequest(handler, "GET", "/api/showcase?location=PETRODVORTSOVY&seed=123", "", token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := fmt.Sprintf(`{"category":"help","revision":%d}`, changed.Revision)
	if w := reviewRequest(handler, "PUT", fmt.Sprintf("/api/admin/listings/%d/category", first.ID), body, token); w.Code != 403 {
		t.Fatal("HTTP role bypass", w.Code)
	}
}

func TestCategoryAndLocationUpgradePreservesExistingAds(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "legacy@students.example", false)
	// Recreate the pre-010 shape in the disposable integration database.
	if _, err := db.Exec(ctx, `ALTER TABLE listings DROP COLUMN category_needs_review CASCADE; DELETE FROM schema_migrations WHERE name IN ('010_categories_and_locations.sql','011_books_category_name.sql')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migrations.Apply(ctx, db) })
	ids := map[string]int64{}
	for _, number := range []string{"8", "9", "11", "12", "13", "20"} {
		var dorm, id int64
		if err := db.QueryRow(ctx, `INSERT INTO dorms(location,name) VALUES('PETRODVORTSOVY',$1) RETURNING id`, "№ "+number).Scan(&dorm); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `INSERT INTO listings(author_id,kind,category,title,description,location,dorm_id,price_mode,telegram,status) VALUES($1,'offer','things','Старая вещь','Описание старой вещи','PETRODVORTSOVY',$2,'unset','https://t.me/student','active') RETURNING id`, owner, dorm).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[number] = id
		if _, err := db.Exec(ctx, `INSERT INTO photos(id,listing_id,position,filename,width,height) VALUES($1,$2,0,$3,640,480)`, "legacy-"+number, id, "legacy-"+number+".jpg"); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	catalog, err := locations.New(db).Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var books bool
	for _, category := range catalog.Categories {
		if category.Code == "study" && category.Name == "Книги и пособия" {
			books = true
		}
	}
	if !books {
		t.Fatal("book category not renamed after upgrade")
	}
	ads := listings.New(db)
	for number, id := range ids {
		l, err := ads.Get(ctx, id, owner, false)
		if err != nil {
			t.Fatal(err)
		}
		want := "PETRODVORTSOVY"
		if number == "8" || number == "9" {
			want = "KHALTURINA"
		}
		if number == "11" {
			want = "GSOM"
		}
		if l.Location != want || l.Category != "other" || !l.CategoryNeedsReview || l.Status != "active" || l.DormID == nil || len(l.Photos) != 1 || l.Photos[0].ID != "legacy-"+number {
			t.Fatalf("legacy data changed incorrectly: %+v", l)
		}
	}
	// Existing migration history makes a second startup a no-op.
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	page, err := ads.ListForViewer(ctx, owner, listings.Filter{Scope: "categorization"}, true)
	if err != nil || page.Total != 6 {
		t.Fatal(page, err)
	}
	if _, err = ads.ListForViewer(ctx, owner, listings.Filter{Scope: "categorization"}, false); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("classification queue is public", err)
	}
	admin := account(t, db, "admin@students.example", true)
	l, err := ads.Get(ctx, ids["12"], admin, true)
	if err != nil {
		t.Fatal(err)
	}
	l, err = ads.SetCategory(ctx, l.ID, admin, l.Revision, "other")
	if err != nil || l.CategoryNeedsReview || l.Status != "active" || len(l.Photos) != 1 {
		t.Fatal("confirming Other changes publication or loses photos", l, err)
	}
	page, err = ads.ListForViewer(ctx, admin, listings.Filter{Scope: "categorization"}, true)
	if err != nil || page.Total != 5 {
		t.Fatal("classified entry remains in queue", page, err)
	}
}
