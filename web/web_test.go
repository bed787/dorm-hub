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
		"/rules":   "За нарушение правил администрация вправе отклонить или удалить объявление",
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
	for _, path := range []string{"/app", "/login", "/admin-login"} {
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

func TestPrivateEntryAndVersionedAssets(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/app", nil))
	// The landing is available only in an inert template for logout. The visible
	// app container must start with a neutral loading screen.
	visible := strings.Split(w.Body.String(), `<template id="public-landing">`)[0]
	if strings.Contains(visible, `id="landing"`) || !strings.Contains(visible, "Открываем Dorm Hub") {
		t.Fatal("private entry contains a visible landing page")
	}
	if !strings.Contains(w.Body.String(), "/assets/app.js?v="+assetVersion) || !strings.Contains(w.Body.String(), "/assets/app.css?v="+assetVersion) {
		t.Fatal("new releases can reuse stale scripts/styles")
	}
	w = httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/about", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `rel="canonical" href="https://dorm-hub.ru/"`) {
		t.Fatal("public about page missing or duplicate canonical")
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
