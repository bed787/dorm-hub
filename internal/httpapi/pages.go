package httpapi

import (
	"dorm-hub/internal/domain"
	"dorm-hub/web"
	"errors"
	"net/http"
)

// Public HTML and the private application have separate entry points. A valid
// session is checked before sending HTML, so a reload cannot flash the landing.
func (a *API) pages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		public := path == "/" || path == "/about" || path == "/rules" || path == "/privacy" || path == "/consent"
		if public && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("Cache-Control", "no-store")
			if cookie, err := r.Cookie(a.cfg.SessionCookie()); err == nil {
				_, err = a.auth.Authenticate(r.Context(), cookie.Value)
				if err == nil {
					if path == "/" {
						http.Redirect(w, r, "/app", http.StatusTemporaryRedirect)
						return
					}
					r = web.ForAuthenticatedUser(r)
				}
				// Public documents stay readable if a session cannot be checked.
				if err != nil && !errors.Is(err, domain.ErrUnauthorized) && path == "/" {
					a.fail(w, r, err)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
