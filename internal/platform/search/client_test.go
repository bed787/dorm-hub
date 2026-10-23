package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestsUseRestrictedKeyAndWaitForTasks(t *testing.T) {
	const master = "test-master-key-at-least-32-bytes-long"
	var ordinary, polled int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/keys/") {
			if r.Header.Get("Authorization") != "Bearer "+master {
				t.Error("bootstrap did not use master")
			}
			fmt.Fprint(w, `{"key":"scoped-key"}`)
			return
		}
		ordinary++
		if r.Header.Get("Authorization") != "Bearer scoped-key" {
			t.Error("regular request used the master key")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/indexes/"+Index:
			fmt.Fprint(w, `{"primaryKey":"id"}`)
		case strings.HasPrefix(r.URL.Path, "/tasks/"):
			polled++
			fmt.Fprint(w, `{"status":"succeeded"}`)
		case strings.HasSuffix(r.URL.Path, "/search"):
			var q struct {
				Q          string   `json:"q"`
				Attributes []string `json:"attributesToRetrieve"`
				Strategy   string   `json:"matchingStrategy"`
			}
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				t.Error(err)
			}
			if q.Q != "холодилник" || q.Strategy != "all" || strings.Join(q.Attributes, ",") != "id,revision" {
				t.Error(q)
			}
			fmt.Fprint(w, `{"hits":[{"id":4,"revision":8}]}`)
		default:
			if r.URL.Path == "/indexes/"+Index+"/settings" {
				var settings map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&settings)
				if string(settings["displayedAttributes"]) != `["id","revision"]` {
					t.Error("text exposed through displayed fields")
				}
			}
			fmt.Fprint(w, `{"taskUid":1}`)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, master)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = client.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if client.Ready() {
		t.Fatal("reset made an incomplete index searchable")
	}
	if err = client.Put(ctx, []Document{{ID: 4, Revision: 8, Title: "Холодильник"}}); err != nil {
		t.Fatal(err)
	}
	if err = client.Delete(ctx, []int64{5}); err != nil {
		t.Fatal(err)
	}
	client.SetReady(true)
	hits, err := client.Search(ctx, "холодилник")
	if err != nil || len(hits) != 1 || hits[0].ID != 4 || polled < 4 || ordinary < 5 {
		t.Fatal(hits, err, polled, ordinary)
	}
}
func TestFailedOrUnfinishedTasksDoNotReportSuccess(t *testing.T) {
	for _, status := range []string{"failed", "canceled", "enqueued", "unexpected"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					fmt.Fprint(w, `{"taskUid":2}`)
				} else {
					fmt.Fprintf(w, `{"status":%q}`, status)
				}
			}))
			defer server.Close()
			client, _ := New(server.URL, strings.Repeat("k", 32))
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			if err := client.Put(ctx, []Document{{ID: 1}}); err == nil {
				t.Fatal("failed or pending task acknowledged")
			}
		})
	}
}
func TestSearchRejectsBadHitsAndDoesNotLeakResponseBodies(t *testing.T) {
	for _, body := range []string{`{"hits":[{"id":1,"revision":0}]}`, `{"hits":[{"id":1,"revision":1},{"id":1,"revision":1}]}`, `{"hits":[{"id":-1,"revision":1}]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c, _ := New(server.URL, strings.Repeat("k", 32))
		c.SetReady(true)
		if _, err := c.Search(context.Background(), "лампа"); err == nil || c.Ready() {
			t.Fatal("invalid hit accepted")
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"code":"internal","message":"PRIVATE TEXT AND SECRET KEY"}`)
	}))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("k", 32))
	c.SetReady(true)
	_, err := c.Search(context.Background(), "test")
	if err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("error body leaked", err)
	}
}
func TestSearchFallbackWhileNotReadyAndAtEngineLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		hits := make([]Hit, MaxHits)
		for i := range hits {
			hits[i] = Hit{ID: int64(i + 1), Revision: 1}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
	}))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("k", 32))
	if _, err := c.Search(context.Background(), "test"); !errors.Is(err, ErrUnavailable) || requests != 0 {
		t.Fatal(err, requests)
	}
	c.SetReady(true)
	if _, err := c.Search(context.Background(), "test"); !errors.Is(err, ErrTooBroad) || !c.Ready() {
		t.Fatal(err)
	}
}
func TestHTTPRedirectCannotForwardCredentials(t *testing.T) {
	received := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("k", 32))
	c.SetReady(true)
	if _, err := c.Search(context.Background(), "test"); err == nil || received {
		t.Fatal("followed redirect with credentials")
	}
}

func TestBootstrapCreatesIndexAndScopedKeyOnlyOnce(t *testing.T) {
	const master = "a-master-key-with-at-least-32-bytes"
	created, indexExists := false, false
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/keys") {
			if r.Header.Get("Authorization") != "Bearer "+master {
				t.Error("key management lacked master key")
			}
			if r.Method == "GET" && !created {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"code":"api_key_not_found"}`)
				return
			}
			if r.Method == "POST" {
				var key struct {
					UID     string   `json:"uid"`
					Actions []string `json:"actions"`
					Indexes []string `json:"indexes"`
					Expires any      `json:"expiresAt"`
				}
				if err := json.NewDecoder(r.Body).Decode(&key); err != nil {
					t.Error(err)
				}
				if key.UID != keyUID || len(key.Indexes) != 1 || key.Indexes[0] != Index || key.Expires != nil {
					t.Error("key was not scoped", key)
				}
				for _, action := range key.Actions {
					if action == "*" || strings.HasPrefix(action, "keys.") {
						t.Error("application key grants key management")
					}
				}
				created = true
				count++
			}
			fmt.Fprint(w, `{"key":"restricted"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer restricted" {
			t.Error("master used for data operation")
		}
		if r.Method == "GET" && r.URL.Path == "/indexes/"+Index {
			if !indexExists {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"code":"index_not_found"}`)
			} else {
				fmt.Fprint(w, `{"primaryKey":"id"}`)
			}
			return
		}
		if r.Method == "POST" && r.URL.Path == "/indexes" {
			indexExists = true
		}
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			fmt.Fprint(w, `{"status":"succeeded"}`)
		} else {
			fmt.Fprint(w, `{"taskUid":0}`)
		}
	}))
	defer server.Close()
	c, _ := New(server.URL, master)
	for i := 0; i < 2; i++ {
		if err := c.Reset(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 || !indexExists {
		t.Fatal("key/index bootstrap was not idempotent", count, indexExists)
	}
}

func TestMalformedTasksAndOversizedResponsesAreRejected(t *testing.T) {
	for _, body := range []string{`{}`, `{"taskUid":-1}`, `{bad json}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c, _ := New(server.URL, strings.Repeat("k", 32))
		if err := c.Put(context.Background(), []Document{{ID: 1}}); err == nil {
			t.Fatal("malformed task accepted")
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", 2*1024*1024+1)) }))
	defer server.Close()
	c, _ := New(server.URL, strings.Repeat("k", 32))
	c.SetReady(true)
	if _, err := c.Search(context.Background(), "test"); err == nil {
		t.Fatal("oversized response accepted")
	}
}
