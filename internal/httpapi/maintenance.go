package httpapi

import (
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"dorm-hub/web"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

type siteSettingsState struct {
	Maintenance bool   `json:"maintenance"`
	Reason      string `json:"reason"`
}

func (a *API) maintenanceStatus(ctx context.Context) (siteSettingsState, error) {
	var state siteSettingsState
	err := a.db.QueryRow(ctx, `SELECT maintenance,maintenance_reason FROM site_settings WHERE singleton=true`).Scan(&state.Maintenance, &state.Reason)
	return state, err
}

func maintenanceResponse(w http.ResponseWriter, r *http.Request, reason string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", "3600")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
				"code": "maintenance", "message": "На сайте технические работы. Пожалуйста, загляните позже.",
				"reason": reason,
			}})
		}
		return
	}
	web.Maintenance(w, r, reason)
}

// The gate runs before all application routes, including photo reads and writes.
// Health probes stay available so deployments do not reopen or stop the site.
func (a *API) maintenanceGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		readOnly := r.Method == http.MethodGet || r.Method == http.MethodHead
		if readOnly && (path == "/health" || path == "/ready" || strings.HasPrefix(path, "/assets/") || path == "/admin-login" || path == "/api/auth/bootstrap") {
			next.ServeHTTP(w, r)
			return
		}
		// Login handlers independently allow only administrator emails while closed.
		if r.Method == http.MethodPost && (path == "/api/auth/request" || path == "/api/auth/verify" || path == "/api/auth/logout") {
			next.ServeHTTP(w, r)
			return
		}
		state, err := a.maintenanceStatus(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
		if state.Maintenance {
			if cookie, err := r.Cookie(a.cfg.SessionCookie()); err == nil {
				session, err := a.auth.Authenticate(r.Context(), cookie.Value)
				if err == nil && session.User.IsAdmin {
					next.ServeHTTP(w, r)
					return
				}
			}
			maintenanceResponse(w, r, state.Reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// This check precedes sending an OTP and creating a user/session. A direct API
// request or the administrator login page cannot register a regular visitor.
func (a *API) allowMaintenanceLogin(w http.ResponseWriter, r *http.Request, email string) (bool, error) {
	state, err := a.maintenanceStatus(r.Context())
	if err != nil || !state.Maintenance {
		return err == nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	allowed := a.cfg.IsAdmin(email)
	if !allowed {
		err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1 AND is_admin AND NOT blocked)`, email).Scan(&allowed)
		if err != nil {
			return false, err
		}
	}
	if !allowed {
		maintenanceResponse(w, r, state.Reason)
	}
	return allowed, nil
}

func (a *API) siteSettings(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	state, err := a.maintenanceStatus(r.Context())
	if err != nil {
		return err
	}
	respond(w, http.StatusOK, state)
	return nil
}

func (a *API) updateSiteSettings(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	var input struct {
		Maintenance *bool  `json:"maintenance"`
		Reason      string `json:"reason"`
	}
	if err := decode(w, r, &input); err != nil {
		return err
	}
	if input.Maintenance == nil {
		return domain.Invalid("Укажите maintenance: true или false")
	}
	reason := strings.TrimSpace(input.Reason)
	if *input.Maintenance {
		if reason == "" || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > 1000 {
			return domain.Invalid("Укажите причину технических работ: от 1 до 1000 символов")
		}
	} else {
		reason = ""
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE site_settings SET maintenance=$1,maintenance_reason=$2,updated_at=now() WHERE singleton=true`, *input.Maintenance, reason); err != nil {
		return err
	}
	respond(w, http.StatusOK, siteSettingsState{Maintenance: *input.Maintenance, Reason: reason})
	return nil
}
