package integration

import (
	"context"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/platform/search"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSearchKey = "integration-test-only-meili-key-32bytes"

type searchFixture struct {
	mu    sync.Mutex
	docs  map[int64]search.Document
	index bool
	fail  bool
	hits  []search.Hit
	hook  func()
}

func (f *searchFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(r.URL.Path, "/keys/") {
		fmt.Fprint(w, `{"key":"restricted-key"}`)
		return
	}
	if f.fail {
		w.WriteHeader(503)
		fmt.Fprint(w, `{"code":"unavailable"}`)
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/indexes/"+search.Index:
		if !f.index {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":"index_not_found"}`)
		} else {
			fmt.Fprint(w, `{"primaryKey":"id"}`)
		}
		return
	case strings.HasPrefix(r.URL.Path, "/tasks/"):
		fmt.Fprint(w, `{"status":"succeeded"}`)
		return
	case strings.HasSuffix(r.URL.Path, "/search"):
		hits := f.hits
		if hits == nil {
			hits = []search.Hit{}
			for _, d := range f.docs {
				hits = append(hits, search.Hit{ID: d.ID, Revision: d.Revision})
			}
			sort.Slice(hits, func(i, j int) bool { return hits[i].ID > hits[j].ID })
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
		return
	case r.Method == "POST" && r.URL.Path == "/indexes":
		f.index = true
	case r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/documents"):
		f.docs = map[int64]search.Document{}
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/documents"):
		var docs []search.Document
		_ = json.NewDecoder(r.Body).Decode(&docs)
		for _, d := range docs {
			f.docs[d.ID] = d
		}
		if f.hook != nil {
			hook := f.hook
			f.hook = nil
			hook()
		}
	case strings.HasSuffix(r.URL.Path, "/documents/delete-batch"):
		var ids []int64
		_ = json.NewDecoder(r.Body).Decode(&ids)
		for _, id := range ids {
			delete(f.docs, id)
		}
	}
	fmt.Fprint(w, `{"taskUid":1}`)
}
func newSearchFixture(t *testing.T) (*searchFixture, *search.Client) {
	t.Helper()
	f := &searchFixture{docs: map[int64]search.Document{}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	engine, err := search.New(server.URL, testSearchKey)
	if err != nil {
		t.Fatal(err)
	}
	return f, engine
}
func searchLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSearchIndexVisibilityFiltersAndStaleRevisions(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "search-owner@students.example", false)
	viewer := account(t, db, "search-viewer@students.example", false)
	ads := listings.New(db)
	first, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	first = publish(t, db, first)
	in := input()
	in.Kind = "request"
	second, err := ads.Create(ctx, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	second = publish(t, db, second)
	pending, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	f, engine := newSearchFixture(t)
	syncer := listings.NewSearchSync(db, engine, searchLogger())
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !engine.Ready() || len(f.docs) != 2 || f.docs[pending.ID].ID != 0 {
		t.Fatal("private listing indexed", f.docs)
	}
	indexed := listings.NewWithSearch(db, engine)
	page, err := indexed.List(ctx, viewer, listings.Filter{Query: "холодилник", Limit: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != second.ID {
		t.Fatal(page, err)
	}
	page, err = indexed.List(ctx, viewer, listings.Filter{Query: "холодилник", Kind: "offer", Limit: 24})
	if err != nil || page.Total != 1 || page.Items[0].ID != first.ID {
		t.Fatal(page, err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO favorites(user_id,listing_id) VALUES($1,$2)`, viewer, first.ID); err != nil {
		t.Fatal(err)
	}
	page, err = indexed.List(ctx, viewer, listings.Filter{Query: "холодилник", Scope: "favorites"})
	if err != nil || page.Total != 1 || page.Items[0].ID != first.ID {
		t.Fatal(page, err)
	}
	// Even a corrupt/stale index cannot expose pending, hidden or wrong revisions.
	f.hits = []search.Hit{{ID: pending.ID, Revision: pending.Revision}, {ID: first.ID, Revision: first.Revision - 1}}
	page, err = indexed.List(ctx, viewer, listings.Filter{Query: "холодилник"})
	if err != nil || page.Total != 0 {
		t.Fatal("stale/private hits escaped SQL", page, err)
	}
	f.hits = nil
	if _, err = ads.Transition(ctx, first.ID, owner, "archive"); err != nil {
		t.Fatal(err)
	}
	page, err = indexed.List(ctx, viewer, listings.Filter{Query: "холодилник", Kind: "offer"})
	if err != nil || page.Total != 0 {
		t.Fatal("archived listing visible during indexing delay", page, err)
	}
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, found := f.docs[first.ID]; found {
		t.Fatal("archived text retained in index")
	}
	if _, err = db.Exec(ctx, `UPDATE users SET blocked=true WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	page, err = indexed.List(ctx, viewer, listings.Filter{Query: "холодилник"})
	if err != nil || page.Total != 0 {
		t.Fatal(page, err)
	}
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.docs) != 0 {
		t.Fatal("blocked author still indexed")
	}
	if _, err = db.Exec(ctx, `UPDATE users SET blocked=false WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.docs) != 1 {
		t.Fatal("unblocked author not restored")
	}
	if _, err = db.Exec(ctx, `DELETE FROM users WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.docs) != 0 {
		t.Fatal("deleted account text retained")
	}
	var jobs int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM search_index_queue`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("deletion tombstones not cleared", jobs, err)
	}
}
func TestSearchFailureFallsBackWithoutLosingQueue(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "fallback@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	l = publish(t, db, l)
	f, engine := newSearchFixture(t)
	syncer := listings.NewSearchSync(db, engine, searchLogger())
	f.fail = true
	if err = syncer.SyncOnce(ctx); err == nil {
		t.Fatal("indexing failure ignored")
	}
	var jobs int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM search_index_queue WHERE listing_id=$1`, l.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatal("queue lost failed update", jobs, err)
	}
	indexed := listings.NewWithSearch(db, engine)
	page, err := indexed.List(ctx, owner, listings.Filter{Query: "лампа"})
	if err != nil || page.Total != 1 {
		t.Fatal("unready index broke existing SQL search", page, err)
	}
	f.fail = false
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.docs) != 1 {
		t.Fatal("failed indexing did not recover")
	}
	f.fail = true
	page, err = indexed.List(ctx, owner, listings.Filter{Query: "лампа"})
	if err != nil || page.Total != 1 || engine.Ready() {
		t.Fatal("failed search broke fallback", page, err)
	}
}
func TestSearchQueueTracksEveryListingMutation(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "queue@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	generation := func() int64 {
		var v int64
		if e := db.QueryRow(ctx, `SELECT version FROM search_index_queue WHERE listing_id=$1`, l.ID).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := generation()
	if _, err = db.Exec(ctx, `UPDATE listings SET hidden=true WHERE id=$1`, l.ID); err != nil {
		t.Fatal(err)
	}
	after := generation()
	if after <= before {
		t.Fatal("moderation did not advance queue generation")
	}
	if err = ads.Delete(ctx, l.ID, owner); err != nil {
		t.Fatal(err)
	}
	if generation() <= after {
		t.Fatal("deletion did not retain a tombstone")
	}
}

// Runs against the pinned real engine in compose.test.yaml and GitHub Actions.
func TestMeilisearchRealEngine(t *testing.T) {
	endpoint := os.Getenv("TEST_MEILI_URL")
	if endpoint == "" {
		t.Skip("TEST_MEILI_URL is not set")
	}
	db := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	owner := account(t, db, "real-search@students.example", false)
	ads := listings.New(db)
	in := input()
	in.Title = "Холодильник Атлант"
	fridge, err := ads.Create(ctx, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	fridge = publish(t, db, fridge)
	in.Title = "Микроволновка Samsung"
	microwave, err := ads.Create(ctx, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	microwave = publish(t, db, microwave)
	engine, err := search.New(endpoint, os.Getenv("TEST_MEILI_MASTER_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	syncer := listings.NewSearchSync(db, engine, searchLogger())
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal("real index bootstrap failed", err)
	}
	indexed := listings.NewWithSearch(db, engine)
	for _, tc := range []struct {
		query string
		id    int64
	}{{"холодилник", fridge.ID}, {"холод", fridge.ID}, {"свч", microwave.ID}} {
		page, e := indexed.List(ctx, owner, listings.Filter{Query: tc.query})
		if e != nil || page.Total != 1 || page.Items[0].ID != tc.id {
			t.Fatalf("query %q: %+v %v", tc.query, page, e)
		}
	}
	if err = ads.Delete(ctx, fridge.ID, owner); err != nil {
		t.Fatal(err)
	}
	// Removal is enforced by PostgreSQL even before the index catches up.
	page, err := indexed.List(ctx, owner, listings.Filter{Query: "холод"})
	if err != nil || page.Total != 0 {
		t.Fatal(page, err)
	}
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	hits, err := engine.Search(ctx, "холод")
	if err != nil || len(hits) != 0 {
		t.Fatal("deleted text retained by engine", hits, err)
	}
}

func TestSearchQueueDoesNotAcknowledgeAnEditMadeDuringIndexing(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	owner := account(t, db, "new-generation@students.example", false)
	ads := listings.New(db)
	l, err := ads.Create(ctx, owner, input())
	if err != nil {
		t.Fatal(err)
	}
	l = publish(t, db, l)
	f, engine := newSearchFixture(t)
	syncer := listings.NewSearchSync(db, engine, searchLogger())
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := pgxpool.NewWithConfig(ctx, db.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = db.Exec(ctx, `UPDATE listings SET title='Первое изменение',revision=revision+1 WHERE id=$1`, l.ID); err != nil {
		t.Fatal(err)
	}
	f.hook = func() {
		change, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, e := second.Exec(change, `UPDATE listings SET title='Следующее изменение',revision=revision+1 WHERE id=$1`, l.ID); e != nil {
			t.Error(e)
		}
	}
	if err = syncer.SyncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if f.docs[l.ID].Title != "Следующее изменение" {
		t.Fatal("newer queue generation was lost", f.docs[l.ID])
	}
}
