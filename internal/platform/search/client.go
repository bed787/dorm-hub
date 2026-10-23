// Package search talks to the internal Meilisearch service. No credentials or
// indexed text are returned to the browser: callers receive IDs and revisions.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const Index = "dormhub_listings"
const MaxHits = 10000
const keyUID = "c15c6bde-a48e-46aa-bf7f-86c9bb4866a0"

var ErrUnavailable = errors.New("search unavailable")
var ErrTooBroad = errors.New("search result exceeds candidate limit")

type Hit struct {
	ID       int64 `json:"id"`
	Revision int64 `json:"revision"`
}
type Document struct {
	ID          int64  `json:"id"`
	Revision    int64  `json:"revision"`
	Title       string `json:"title"`
	Description string `json:"description"`
}
type Client struct {
	base, master string
	http         *http.Client
	mu           sync.RWMutex
	key          string
	ready        atomic.Bool
}
type responseError struct {
	status int
	code   string
}

func (e *responseError) Error() string {
	return fmt.Sprintf("Meilisearch HTTP %d (%s)", e.status, e.code)
}
func New(base, master string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || len(master) < 32 {
		return nil, errors.New("invalid Meilisearch configuration")
	}
	return &Client{base: base, master: master, http: &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Ready() bool         { return c.ready.Load() }
func (c *Client) SetReady(ready bool) { c.ready.Store(ready) }
func (c *Client) request(ctx context.Context, method, path string, input, output any, master bool) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	c.mu.RLock()
	key := c.key
	c.mu.RUnlock()
	if master {
		key = c.master
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return ErrUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var detail struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(data, &detail)
		// Never include response bodies: errors can quote indexed text or secrets.
		code := "error"
		if detail.Code != "" && len(detail.Code) < 80 && strings.Trim(detail.Code, "abcdefghijklmnopqrstuvwxyz_") == "" {
			code = detail.Code
		}
		return &responseError{status: resp.StatusCode, code: code}
	}
	if output != nil {
		if err = json.Unmarshal(data, output); err != nil {
			return errors.New("invalid Meilisearch response")
		}
	}
	return nil
}
func (c *Client) bootstrap(ctx context.Context) error {
	var result struct {
		Key string `json:"key"`
	}
	err := c.request(ctx, "GET", "/keys/"+keyUID, nil, &result, true)
	var failure *responseError
	if errors.As(err, &failure) && failure.status == 404 {
		input := map[string]any{"uid": keyUID, "name": "Dorm Hub application", "actions": []string{"search", "indexes.create", "indexes.get", "documents.add", "documents.delete", "settings.update", "tasks.get"}, "indexes": []string{Index}, "expiresAt": nil}
		err = c.request(ctx, "POST", "/keys", input, &result, true)
		if errors.As(err, &failure) && failure.status == 409 {
			err = c.request(ctx, "GET", "/keys/"+keyUID, nil, &result, true)
		}
	}
	if err != nil {
		return err
	}
	if result.Key == "" {
		return errors.New("Meilisearch returned an empty application key")
	}
	c.mu.Lock()
	c.key = result.Key
	c.mu.Unlock()
	return nil
}
func (c *Client) task(ctx context.Context, method, path string, input any) error {
	var accepted struct {
		UID *int64 `json:"taskUid"`
	}
	if err := c.request(ctx, method, path, input, &accepted, false); err != nil {
		return err
	}
	if accepted.UID == nil || *accepted.UID < 0 {
		return errors.New("Meilisearch did not return a task")
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var task struct {
			Status string `json:"status"`
		}
		if err := c.request(ctx, "GET", "/tasks/"+strconv.FormatInt(*accepted.UID, 10), nil, &task, false); err != nil {
			return err
		}
		switch task.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			return errors.New("Meilisearch indexing task failed")
		case "enqueued", "processing":
		default:
			return errors.New("Meilisearch returned an unknown task status")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Reset runs only during initial recovery/rebuild, while SQL search stays live.
// The master key is used only to provision the restricted application key.
func (c *Client) Reset(ctx context.Context) error {
	c.SetReady(false)
	if err := c.bootstrap(ctx); err != nil {
		return err
	}
	var index struct {
		PrimaryKey string `json:"primaryKey"`
	}
	err := c.request(ctx, "GET", "/indexes/"+Index, nil, &index, false)
	var failure *responseError
	if errors.As(err, &failure) && failure.status == 404 {
		if err = c.task(ctx, "POST", "/indexes", map[string]any{"uid": Index, "primaryKey": "id"}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if index.PrimaryKey != "id" {
		return errors.New("unexpected Meilisearch primary key")
	}
	settings := map[string]any{
		"searchableAttributes": []string{"title", "description"},
		"displayedAttributes":  []string{"id", "revision"},
		"pagination":           map[string]int{"maxTotalHits": MaxHits},
		"typoTolerance":        map[string]any{"enabled": true, "disableOnNumbers": true},
		"synonyms": map[string][]string{
			"холодос": {"холодильник"}, "холодильник": {"холодос"},
			"микроволновка": {"свч", "микроволновая печь"}, "свч": {"микроволновка", "микроволновая печь"}, "микроволновая печь": {"микроволновка", "свч"},
			"общага": {"общежитие"}, "общежитие": {"общага"},
		},
	}
	if err = c.task(ctx, "PATCH", "/indexes/"+Index+"/settings", settings); err != nil {
		return err
	}
	return c.task(ctx, "DELETE", "/indexes/"+Index+"/documents", nil)
}
func (c *Client) Put(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}
	return c.task(ctx, "POST", "/indexes/"+Index+"/documents", docs)
}
func (c *Client) Delete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return c.task(ctx, "POST", "/indexes/"+Index+"/documents/delete-batch", ids)
}
func (c *Client) Search(ctx context.Context, q string) ([]Hit, error) {
	if !c.Ready() {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	var result struct {
		Hits []Hit `json:"hits"`
	}
	err := c.request(ctx, "POST", "/indexes/"+Index+"/search", map[string]any{"q": q, "limit": MaxHits, "attributesToRetrieve": []string{"id", "revision"}, "matchingStrategy": "all"}, &result, false)
	if err != nil {
		c.SetReady(false)
		return nil, err
	}
	// Fall back to SQL rather than silently losing matches beyond the engine cap.
	if len(result.Hits) >= MaxHits {
		return nil, ErrTooBroad
	}
	seen := map[int64]bool{}
	for _, hit := range result.Hits {
		if hit.ID < 1 || hit.Revision < 1 || seen[hit.ID] {
			c.SetReady(false)
			return nil, errors.New("invalid Meilisearch hit")
		}
		seen[hit.ID] = true
	}
	return result.Hits, nil
}
