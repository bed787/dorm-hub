// Package logview keeps a bounded, process-local copy of application events.
// It never reads host files, Docker sockets, request bodies or credentials.
package logview

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const Capacity = 500

type Entry struct {
	ID      uint64    `json:"id"`
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Details string    `json:"details"`
}

type Snapshot struct {
	StartedAt time.Time `json:"started_at"`
	Capacity  int       `json:"capacity"`
	Retained  int       `json:"retained"`
	Matched   int       `json:"matched"`
	Items     []Entry   `json:"items"`
}

type Buffer struct {
	mu      sync.Mutex
	started time.Time
	entries [Capacity]Entry
	next    uint64
	count   int
}

func New() *Buffer { return &Buffer{started: time.Now().UTC()} }

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Converting to runes also avoids cutting a UTF-8 character in half.
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// Write receives one JSON record from slog.JSONHandler. Only explicitly
// selected operational attributes reach this writer (see newHandler below).
func (b *Buffer) Write(p []byte) (int, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(p, &fields); err != nil {
		return len(p), nil
	}
	e := Entry{Time: time.Now().UTC()}
	_ = json.Unmarshal(fields["time"], &e.Time)
	_ = json.Unmarshal(fields["level"], &e.Level)
	_ = json.Unmarshal(fields["msg"], &e.Message)
	delete(fields, "time")
	delete(fields, "level")
	delete(fields, "msg")
	details, _ := json.Marshal(fields)
	e.Message = shorten(e.Message, 512)
	e.Level = shorten(e.Level, 16)
	e.Details = shorten(string(details), 2048)
	b.mu.Lock()
	b.next++
	e.ID = b.next
	b.entries[(b.next-1)%Capacity] = e
	if b.count < Capacity {
		b.count++
	}
	b.mu.Unlock()
	return len(p), nil
}

// Read returns independent values, newest first. An empty level matches all;
// WARN includes errors. Search is literal and never a regular expression.
func (b *Buffer) Read(level, query string) Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := Snapshot{StartedAt: b.started, Capacity: Capacity, Retained: b.count, Items: []Entry{}}
	query = strings.ToLower(query)
	for i := 0; i < b.count; i++ {
		e := b.entries[(b.next-1-uint64(i))%Capacity]
		if level != "" && e.Level != level && !(level == "WARN" && e.Level == "ERROR") {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(e.Message+" "+e.Details+" "+e.Level), query) {
			continue
		}
		result.Matched++
		if len(result.Items) < 200 {
			result.Items = append(result.Items, e)
		}
	}
	return result
}

type handler struct{ next, capture slog.Handler }

// NewHandler preserves the original logger output. The web copy uses an allow
// list: emails, tokens, arbitrary error strings and future unknown attributes
// are not copied into the web viewer. Messages should remain fixed event names.
func NewHandler(next slog.Handler, buffer *Buffer) slog.Handler {
	capture := slog.NewJSONHandler(buffer, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) != 0 {
				return slog.Attr{}
			}
			switch a.Key {
			case "time", "level", "msg", "request_id", "method", "route", "status", "duration_ms", "address", "photo_id", "error_type", "panic_type":
				if a.Value.Kind() == slog.KindString {
					a.Value = slog.StringValue(shorten(a.Value.String(), 512))
				}
				return a
			default:
				return slog.Attr{}
			}
		},
	})
	return &handler{next: next, capture: capture}
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	err := h.next.Handle(ctx, r)
	_ = h.capture.Handle(ctx, r)
	return err
}
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{next: h.next.WithAttrs(attrs), capture: h.capture.WithAttrs(attrs)}
}
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), capture: h.capture.WithGroup(name)}
}
