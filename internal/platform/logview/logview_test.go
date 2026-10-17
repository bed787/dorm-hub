package logview

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestBoundedJournalFiltersAndConcurrentWrites(t *testing.T) {
	b := New()
	logger := slog.New(NewHandler(slog.NewJSONHandler(&bytes.Buffer{}, nil), b))
	for i := 0; i < Capacity+10; i++ {
		logger.Info(fmt.Sprintf("entry %d", i))
	}
	page := b.Read("", "")
	if page.Retained != Capacity || page.Matched != Capacity || len(page.Items) != 200 || page.Items[0].ID != Capacity+10 {
		t.Fatal("incorrect retention or page bound", page.Retained, page.Matched, len(page.Items))
	}
	if page = b.Read("", "entry 0"); page.Matched != 0 {
		t.Fatal("overwritten entry retained")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				logger.Warn("worker warning")
				_ = b.Read("ERROR", "")
			}
		}()
	}
	wg.Wait()
	logger.Error("DATABASE FAILED", "request_id", "request-42")
	page = b.Read("WARN", "database failed")
	if page.Matched != 1 || page.Items[0].Level != "ERROR" {
		t.Fatal("warning filter excluded errors")
	}
	if b.Read("ERROR", "request-42").Matched != 1 {
		t.Fatal("request id cannot be found")
	}
	if b.Read("INFO", "database failed").Matched != 0 {
		t.Fatal("level filter ignored")
	}
}

func TestWebCopyKeepsOperationalFieldsAndPreservesOriginalOutput(t *testing.T) {
	b := New()
	var original bytes.Buffer
	logger := slog.New(NewHandler(slog.NewJSONHandler(&original, nil), b)).With("email", "private@example.test", "request_id", "req-1")
	logger.Error("api error", "status", 500, "token", "secret-token", "error", "credential-bearing error", "route", "GET /api/listings/{id}")
	logger.WithGroup("unknown").Info("group event", "email", "also-private@example.test")
	page := b.Read("", "")
	if len(page.Items) != 2 {
		t.Fatal("missing events")
	}
	for _, e := range page.Items {
		for _, sensitive := range []string{"private@", "secret-token", "credential-bearing", "also-private"} {
			if strings.Contains(e.Details, sensitive) {
				t.Fatal("sensitive attribute in web copy", e.Details)
			}
		}
	}
	if !strings.Contains(page.Items[1].Details, "req-1") || !strings.Contains(page.Items[1].Details, "500") {
		t.Fatal("operational fields lost")
	}
	if !strings.Contains(original.String(), "secret-token") {
		t.Fatal("original logger changed")
	}
	logger.Info(strings.Repeat("я", 2000), "route", strings.Repeat("x", 10000))
	if len([]rune(b.Read("", "").Items[0].Message)) > 512 {
		t.Fatal("entry size not bounded")
	}
}
