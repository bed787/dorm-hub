package httpapi

import (
	"context"
	"crypto/hmac"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/config"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/favorites"
	"dorm-hub/internal/httpapi/middleware"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/locations"
	"dorm-hub/internal/media"
	"dorm-hub/internal/moderation"
	"dorm-hub/internal/stars"
	"dorm-hub/internal/users"
	"dorm-hub/web"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type API struct {
	cfg        config.Config
	logger     *slog.Logger
	db         *pgxpool.Pool
	auth       *auth.Service
	users      *users.Service
	listings   *listings.Service
	locations  *locations.Service
	favorites  *favorites.Service
	stars      *stars.Service
	moderation *moderation.Service
	media      *media.Service
	limiter    *limiter
}

func New(cfg config.Config, logger *slog.Logger, db *pgxpool.Pool, login *auth.Service, profiles *users.Service, ads *listings.Service, catalog *locations.Service, fav *favorites.Service, ratings *stars.Service, mod *moderation.Service, photos *media.Service) http.Handler {
	a := &API{cfg: cfg, logger: logger, db: db, auth: login, users: profiles, listings: ads, locations: catalog, favorites: fav, stars: ratings, moderation: mod, media: photos, limiter: &limiter{items: map[string]bucket{}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			http.Error(w, "not ready", 503)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
	route := func(pattern string, required, admin bool, h endpoint) {
		mux.Handle(pattern, a.wrap(required, admin, h))
	}
	route("GET /api/auth/bootstrap", false, false, a.bootstrap)
	route("POST /api/auth/request", false, false, a.requestCode)
	route("POST /api/auth/verify", false, false, a.verifyCode)
	route("POST /api/auth/admin/request", false, false, a.requestAdminCode)
	route("POST /api/auth/admin/verify", false, false, a.verifyAdminCode)
	route("POST /api/auth/logout", true, false, a.logout)
	route("GET /api/me", true, false, a.me)
	route("PUT /api/me", true, false, a.updateMe)
	route("POST /api/me/deletion-code", true, false, a.requestDeletionCode)
	route("DELETE /api/me", true, false, a.deleteAccount)
	route("GET /api/statistics", true, false, a.statistics)
	route("GET /api/admin/site-settings", true, true, a.siteSettings)
	route("PUT /api/admin/site-settings", true, true, a.updateSiteSettings)
	route("GET /api/admin/listings", true, true, a.reviewQueue)
	route("POST /api/admin/listings/{id}/review", true, true, a.reviewListing)
	route("DELETE /api/listings/{id}", true, false, a.deleteListing)
	route("GET /api/catalog", true, false, a.catalog)
	route("GET /api/users/{id}", true, false, a.profile)
	route("PUT /api/users/{id}/star", true, false, a.star)
	route("DELETE /api/users/{id}/star", true, false, a.star)
	route("GET /api/listings", true, false, a.feed)
	route("POST /api/listings", true, false, a.createListing)
	route("GET /api/listings/{id}", true, false, a.getListing)
	route("PUT /api/listings/{id}", true, false, a.updateListing)
	route("POST /api/listings/{id}/status", true, false, a.transition)
	route("PUT /api/listings/{id}/favorite", true, false, a.favorite)
	route("DELETE /api/listings/{id}/favorite", true, false, a.favorite)
	route("POST /api/listings/{id}/photos", true, false, a.uploadPhoto)
	route("PUT /api/listings/{id}/photos", true, false, a.reorderPhotos)
	route("DELETE /api/listings/{id}/photos/{photo}", true, false, a.deletePhoto)
	route("GET /api/photos/{photo}", true, false, a.getPhoto)
	route("POST /api/reports", true, false, a.report)
	route("GET /api/admin/reports", true, true, a.reports)
	route("PUT /api/admin/reports/{id}", true, true, a.resolveReport)
	route("GET /api/admin/users", true, true, a.accounts)
	route("POST /api/admin/actions", true, true, a.moderate)
	route("POST /api/admin/dorms", true, true, a.createDorm)
	route("DELETE /api/admin/dorms/{id}", true, true, a.deleteDorm)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { a.fail(w, r, domain.ErrNotFound) })
	mux.Handle("/", a.pages(web.HandlerFor(cfg.BaseURL)))
	return middleware.Wrap(a.maintenanceGate(mux), logger)
}

type endpoint func(http.ResponseWriter, *http.Request, auth.Session) error

func (a *API) wrap(required, admin bool, h endpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		unsafe := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
		if unsafe && !a.allowedOrigin(r) {
			a.fail(w, r, domain.Forbidden("Недопустимый источник запроса"))
			return
		}
		var session auth.Session
		if required {
			cookie, err := r.Cookie(a.cfg.SessionCookie())
			if err != nil {
				a.fail(w, r, domain.ErrUnauthorized)
				return
			}
			session, err = a.auth.Authenticate(r.Context(), cookie.Value)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			if admin && !session.User.IsAdmin {
				a.fail(w, r, domain.ErrForbidden)
				return
			}
			if unsafe && !validCSRF(session.CSRF, r.Header.Get("X-CSRF-Token")) {
				a.fail(w, r, domain.CSRF("Защита запроса: обновите страницу"))
				return
			}
		} else if unsafe {
			cookie, err := r.Cookie(a.loginCookie())
			if err != nil || !validCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
				a.fail(w, r, domain.CSRF("Обновите страницу входа"))
				return
			}
			host := a.clientIP(r)
			if !a.limiter.allow(host) {
				a.fail(w, r, domain.ErrRateLimit)
				return
			}
		}
		if required && unsafe && !a.allowMutation(r, session.User.ID) {
			a.fail(w, r, &domain.Error{Kind: domain.ErrRateLimit, Message: "Достигнут лимит запросов аккаунта. Повторите позже"})
			return
		}
		if err := h(w, r, session); err != nil {
			a.fail(w, r, err)
		}
	})
}

// Production is pinned to BASE_URL. Local development may also use the
// private/loopback address that actually served the page. Forwarded headers
// do not grant access and CSRF/session checks still run afterwards.
func (a *API) allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	if sameOrigin(origin, a.cfg.BaseURL) {
		return true
	}
	if a.cfg.Env != "development" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	target := scheme + "://" + r.Host
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	local := strings.EqualFold(host, "localhost") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()))
	return local && sameOrigin(origin, target)
}
func sameOrigin(left, right string) bool {
	parse := func(value string) (string, string, string, bool) {
		u, err := url.Parse(value)
		if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "", "", "", false
		}
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		return u.Scheme, strings.ToLower(u.Hostname()), port, true
	}
	ls, lh, lp, lok := parse(left)
	rs, rh, rp, rok := parse(right)
	return lok && rok && ls == rs && lh == rh && lp == rp
}

func validCSRF(expected, provided string) bool {
	return len(expected) == 64 && len(provided) == 64 && hmac.Equal([]byte(expected), []byte(provided))
}
func (a *API) loginCookie() string {
	if a.cfg.SecureCookies {
		return "__Host-dormhub_login"
	}
	return "dormhub_login"
}
func (a *API) setSession(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: a.cfg.SessionCookie(), Value: token, Path: "/", HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
}
func (a *API) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: a.cfg.SessionCookie(), Value: "", Path: "/", HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

type bucket struct {
	expires time.Time
	count   int
}
type limiter struct {
	mu    sync.Mutex
	items map[string]bucket
}

func (l *limiter) allow(key string) bool { return l.allowWithin("login:"+key, 300, 15*time.Minute) }
func (l *limiter) allowWithin(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.items == nil {
		l.items = map[string]bucket{}
	}
	if _, known := l.items[key]; !known && len(l.items) >= 10000 {
		for k, v := range l.items {
			if !now.Before(v.expires) {
				delete(l.items, k)
			}
		}
		if len(l.items) >= 10000 {
			return false
		}
	}
	b := l.items[key]
	if !now.Before(b.expires) {
		b = bucket{expires: now.Add(window)}
	}
	if b.count >= limit {
		return false
	}
	b.count++
	l.items[key] = b
	return true
}
func (a *API) allowMutation(r *http.Request, user int64) bool {
	var name string
	var limit int
	switch r.Pattern {
	case "POST /api/listings":
		name, limit = "listing", 30
	case "POST /api/listings/{id}/photos":
		name, limit = "photo", 120
	case "POST /api/me/deletion-code":
		name, limit = "deletion-code", 10
	case "POST /api/reports":
		name, limit = "report", 10
	default:
		return true
	}
	return a.limiter.allowWithin(name+":"+strconv.FormatInt(user, 10), limit, time.Hour)
}
func filter(r *http.Request) (listings.Filter, error) {
	q := r.URL.Query()
	limit, err := integer(r, "limit", 24)
	if err != nil {
		return listings.Filter{}, err
	}
	offset, err := integer(r, "offset", 0)
	if err != nil {
		return listings.Filter{}, err
	}
	dorm, err := integer(r, "dorm_id", 0)
	if err != nil {
		return listings.Filter{}, err
	}
	author, err := integer(r, "author_id", 0)
	if err != nil {
		return listings.Filter{}, err
	}
	return listings.Filter{Query: strings.TrimSpace(q.Get("q")), Kind: q.Get("kind"), Category: q.Get("category"), Location: q.Get("location"), Scope: q.Get("scope"), Status: q.Get("status"), DormID: int64(dorm), AuthorID: int64(author), Limit: limit, Offset: offset}, nil
}

func (a *API) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	trusted := func(ip net.IP) bool {
		for _, network := range a.cfg.TrustedProxies {
			if network.Contains(ip) {
				return true
			}
		}
		return false
	}
	peer := net.ParseIP(host)
	if peer == nil || !trusted(peer) {
		return host
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(chain) > 16 {
		return host
	}
	candidate := peer
	for index := len(chain) - 1; index >= 0; index-- {
		if !trusted(candidate) {
			break
		}
		next := net.ParseIP(strings.TrimSpace(chain[index]))
		if next == nil {
			return host
		}
		candidate = next
	}
	return candidate.String()
}
