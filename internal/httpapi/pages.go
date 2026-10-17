package httpapi

import (
	"dorm-hub/internal/domain"
	"errors"
	"net/http"
)

// Public HTML and the private application have separate entry points. A valid
// session is checked before sending HTML, so a reload cannot flash the landing.
func (a *API) pages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			w.Header().Set("Cache-Control", "no-store")
			if cookie, err := r.Cookie(a.cfg.SessionCookie()); err == nil {
				_, err = a.auth.Authenticate(r.Context(), cookie.Value)
				if err == nil {
					http.Redirect(w, r, "/app", http.StatusTemporaryRedirect)
					return
				}
				if !errors.Is(err, domain.ErrUnauthorized) {
					a.fail(w, r, err)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
