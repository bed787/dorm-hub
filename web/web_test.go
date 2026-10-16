package web

import (
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicContentWithoutJavaScript(t *testing.T) {
	handler := HandlerFor("https://dorm.example")
	for path, text := range map[string]string{
		"/":        "студенческая доска объявлений СПбГУ",
		"/rules":   "За любое нарушение этих правил аккаунт блокируется",
		"/privacy": "При удалении аккаунта удаляются",
		"/consent": "Просмотр этой страницы сам по себе не означает согласия",
	} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "https://spoof.example"+path, nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), text) {
				t.Fatalf("missing public text: %d %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `rel="canonical" href="https://dorm.example`+path+`"`) {
				t.Fatal("canonical did not use configured URL")
			}
			if strings.Contains(w.Body.String(), `name="robots" content="noindex`) {
				t.Fatal("public page excludes indexing")
			}
			if path == "/" && strings.Count(w.Body.String(), "<details>") < 8 {
				t.Fatal("FAQ unavailable to a crawler")
			}
		})
	}
	for _, path := range []string{"/login", "/admin-login"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("X-Robots-Tag"), "noindex") {
			t.Fatal("login should not be indexed", path)
		}
	}
	for _, path := range []string{"/", "/rules", "/privacy", "/consent", "/robots.txt", "/sitemap.xml"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("HEAD", path, nil))
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Fatal("HEAD body/status", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/not-a-page", nil))
	if w.Code != 404 {
		t.Fatal("unknown pages should return 404")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/sitemap.xml", nil))
	if !strings.Contains(w.Body.String(), "https://dorm.example/privacy") || strings.Contains(w.Body.String(), "/api/") {
		t.Fatal("incorrect sitemap", w.Body.String())
	}
}

func TestConsentSnapshotMatchesVisibleText(t *testing.T) {
	docs := CurrentLegal()
	if docs.ConsentVersion != fmt.Sprintf("%x", sha256.Sum256([]byte(docs.ConsentText))) {
		t.Fatal("consent hash does not match content")
	}
	if docs.RulesVersion != fmt.Sprintf("%x", sha256.Sum256([]byte(docs.RulesText))) {
		t.Fatal("rules hash does not match content")
	}
	for path, text := range map[string]string{"/consent": docs.ConsentText, "/rules": docs.RulesText} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if !strings.Contains(w.Body.String(), text) {
			t.Fatal("persisted snapshot differs from public document", path)
		}
	}
}
