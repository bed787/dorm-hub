package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed index.html maintenance.html assets/*
var files embed.FS

func Handler() http.Handler {
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
		if r.URL.Path != "/" && r.URL.Path != "/admin-login" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		data, err := files.ReadFile("index.html")
		if err != nil {
			http.Error(w, "Internal error", 500)
			return
		}
		if r.Method != "HEAD" {
			_, _ = w.Write(data)
		}
	})
}

func Maintenance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "3600")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		data, _ := files.ReadFile("maintenance.html")
		_, _ = w.Write(data)
	}
}
