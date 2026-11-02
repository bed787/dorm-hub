package httpapi

import (
	"dorm-hub/internal/domain"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Grafana keeps its own login. The proxy also requires a current Dorm Hub admin
// session, including for Grafana's assets and API. No auth-proxy identity headers.
func (a *API) monitoringProxy() http.Handler {
	if a.cfg.MonitoringURL == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	}
	target, err := url.Parse(a.cfg.MonitoringURL)
	if err != nil || target.Host == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "Monitoring unavailable", 503) })
	}
	public, _ := url.Parse(a.cfg.BaseURL)
	proxy := &httputil.ReverseProxy{Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(target)
		p.Out.Host = public.Host
		p.Out.Header.Set("X-Forwarded-Proto", public.Scheme)
		p.Out.Header.Set("X-Forwarded-Host", public.Host)
		p.Out.Header.Del("X-Forwarded-For")
		p.Out.Header.Del("Authorization")
		p.Out.Header.Del("X-CSRF-Token")
		for name := range p.Out.Header {
			if strings.HasPrefix(strings.ToLower(name), "x-webauth-") {
				p.Out.Header.Del(name)
			}
		}
		p.Out.Header.Del("Cookie")
		for _, c := range p.In.Cookies() {
			if strings.HasPrefix(c.Name, "grafana_") {
				p.Out.AddCookie(c)
			}
		}
	}, ModifyResponse: func(r *http.Response) error { r.Header.Set("Cache-Control", "no-store"); return nil },
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, _ error) {
			http.Error(w, "Мониторинг временно недоступен", 503)
		}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(a.cfg.SessionCookie())
		if err != nil {
			http.Redirect(w, r, "/admin-login", http.StatusSeeOther)
			return
		}
		session, err := a.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if !session.User.IsAdmin {
			a.fail(w, r, domain.ErrForbidden)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && !a.allowedOrigin(r) {
			a.fail(w, r, domain.ErrForbidden)
			return
		}
		// Grafana supplies its own CSP, including the nonce for its boot script.
		w.Header().Del("Content-Security-Policy")
		proxy.ServeHTTP(w, r)
	})
}
