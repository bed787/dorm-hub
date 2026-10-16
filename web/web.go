package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

//go:embed index.html maintenance.html landing.html legal.html document.html site.json assets/*
var files embed.FS

var maintenancePage = template.Must(template.ParseFS(files, "maintenance.html"))
var publicPages = template.Must(template.ParseFS(files, "index.html", "landing.html", "document.html", "legal.html"))

type siteContent struct {
	Campus          string `json:"campus"`
	City            string `json:"city"`
	SupportTelegram string `json:"support_telegram"`
	TelegramURL     string `json:"-"`
}

var site = func() siteContent {
	data, err := files.ReadFile("site.json")
	if err != nil {
		panic(err)
	}
	var result siteContent
	if err := json.Unmarshal(data, &result); err != nil {
		panic(err)
	}
	if value := strings.TrimSpace(result.SupportTelegram); value != "" {
		value = strings.TrimPrefix(value, "https://t.me/")
		value = strings.TrimPrefix(value, "t.me/")
		value = strings.TrimPrefix(value, "@")
		if !regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`).MatchString(value) {
			panic("web/site.json: support_telegram must be a Telegram username or https://t.me/username")
		}
		result.SupportTelegram = "@" + value
		result.TelegramURL = "https://t.me/" + value
	}
	return result
}()

// Snapshot includes the exact public text shown by this build. Content hashes
// change when text or operator details change, even if the human date does not.
type LegalSnapshot struct {
	ConsentVersion string `json:"consent_version"`
	RulesVersion   string `json:"rules_version"`
	ConsentText    string `json:"consent_text"`
	RulesText      string `json:"rules_text"`
}

var snapshot = func() LegalSnapshot {
	render := func(name string) string {
		var out bytes.Buffer
		if err := publicPages.ExecuteTemplate(&out, name, site); err != nil {
			panic(err)
		}
		return out.String()
	}
	rules, consent := render("rules"), render("consent")
	version := func(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }
	return LegalSnapshot{RulesVersion: version(rules), ConsentVersion: version(consent), RulesText: rules, ConsentText: consent}
}()

func CurrentLegal() LegalSnapshot { return snapshot }

type pageData struct {
	siteContent
	Page, Title, Description, Canonical string
	Landing                             bool
}

func Handler() http.Handler {
	return HandlerFor("https://dorm-hub.ru")
}

func HandlerFor(baseURL string) http.Handler {
	assets, _ := fs.Sub(files, "assets")
	static := http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", 405)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			static.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		if path == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if r.Method != "HEAD" {
				_, _ = fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /login\nDisallow: /admin-login\nSitemap: %s/sitemap.xml\n", baseURL)
			}
			return
		}
		if path == "/sitemap.xml" {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			if r.Method != "HEAD" {
				_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
				for _, suffix := range []string{"/", "/rules", "/privacy", "/consent"} {
					_, _ = fmt.Fprint(w, "<url><loc>")
					_ = xml.EscapeText(w, []byte(baseURL+suffix))
					_, _ = fmt.Fprint(w, "</loc></url>")
				}
				_, _ = fmt.Fprint(w, "</urlset>")
			}
			return
		}
		if path != "/" && path != "/admin-login" && path != "/login" && path != "/rules" && path != "/privacy" && path != "/consent" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		data := pageData{siteContent: site, Canonical: baseURL + path, Landing: path == "/",
			Title:       "Dorm Hub — студенческая барахолка " + site.Campus,
			Description: "Объявления студентов " + site.Campus + ": учебники, техника, вещи для общежития и взаимопомощь. Размещай предложения и связывайся напрямую."}
		name := "index.html"
		switch path {
		case "/login", "/admin-login":
			data.Title = "Вход — Dorm Hub"
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		case "/rules", "/privacy", "/consent":
			name, data.Page = "document.html", strings.TrimPrefix(path, "/")
			data.Title = map[string]string{"/rules": "Правила сайта", "/privacy": "Политика обработки персональных данных", "/consent": "Согласие на обработку персональных данных"}[path]
			data.Description = data.Title + " на площадке Dorm Hub для студентов " + site.Campus + "."
		}
		var out bytes.Buffer
		if err := publicPages.ExecuteTemplate(&out, name, data); err != nil {
			http.Error(w, "Internal error", 500)
			return
		}
		if r.Method != "HEAD" {
			_, _ = w.Write(out.Bytes())
		}
	})
}

func Maintenance(w http.ResponseWriter, r *http.Request, reason string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "3600")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		if strings.TrimSpace(reason) == "" {
			reason = "Сайт временно закрыт. Пожалуйста, загляните позже."
		}
		_ = maintenancePage.Execute(w, struct{ Reason string }{Reason: reason})
	}
}
