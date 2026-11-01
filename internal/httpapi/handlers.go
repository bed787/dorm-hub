package httpapi

import (
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/media"
	"dorm-hub/internal/platform/idempotency"
	"dorm-hub/internal/users"
	"dorm-hub/web"
	"io"
	"net/http"
	"time"
)

func (a *API) bootstrap(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	token := ""
	if cookie, err := r.Cookie(a.loginCookie()); err == nil && auth.ValidToken(cookie.Value) {
		token = cookie.Value
	}
	if token == "" {
		var err error
		token, err = auth.Token()
		if err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{Name: a.loginCookie(), Value: token, Path: "/", HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 1800})
	documents := web.CurrentLegal()
	respond(w, 200, map[string]any{"csrf": token, "legal": map[string]string{"consent_version": documents.ConsentVersion, "rules_version": documents.RulesVersion}})
	return nil
}
func (a *API) requestCode(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	return a.requestLoginCode(w, r, false)
}
func (a *API) requestAdminCode(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	return a.requestLoginCode(w, r, true)
}
func (a *API) requestLoginCode(w http.ResponseWriter, r *http.Request, adminOnly bool) error {
	var in struct {
		Email          string `json:"email"`
		Consent        bool   `json:"consent"`
		Rules          bool   `json:"rules"`
		ConsentVersion string `json:"consent_version"`
		RulesVersion   string `json:"rules_version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if adminOnly {
		if err := a.requireAdminEmail(r.Context(), in.Email); err != nil {
			return err
		}
	}
	if allowed, err := a.allowMaintenanceLogin(w, r, in.Email); err != nil || !allowed {
		return err
	}

	if adminOnly {
		if err := a.auth.Request(r.Context(), in.Email); err != nil {
			return err
		}
		respond(w, 200, map[string]bool{"sent": true})
		return nil
	}
	documents := web.CurrentLegal()
	if !in.Consent || !in.Rules {
		return domain.Invalid("Подтвердите согласие на обработку данных и примите правила сайта")
	}
	if in.ConsentVersion != documents.ConsentVersion || in.RulesVersion != documents.RulesVersion {
		return domain.Conflict("Тексты документов обновились. Обновите страницу и подтвердите их заново")
	}
	acceptance := auth.Acceptance{ConsentVersion: documents.ConsentVersion, RulesVersion: documents.RulesVersion, ConsentText: documents.ConsentText, RulesText: documents.RulesText, AcceptedAt: time.Now().UTC()}
	if err := a.auth.RequestWithAcceptance(r.Context(), in.Email, acceptance); err != nil {
		return err
	}
	respond(w, 200, map[string]bool{"sent": true})
	return nil
}
func (a *API) verifyCode(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	return a.verifyLoginCode(w, r, false)
}
func (a *API) verifyAdminCode(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	return a.verifyLoginCode(w, r, true)
}
func (a *API) verifyLoginCode(w http.ResponseWriter, r *http.Request, adminOnly bool) error {
	var in struct {
		Email      string `json:"email"`
		Code       string `json:"code"`
		Reactivate bool   `json:"reactivate"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if adminOnly {
		if err := a.requireAdminEmail(r.Context(), in.Email); err != nil {
			return err
		}
	}
	if allowed, err := a.allowMaintenanceLogin(w, r, in.Email); err != nil || !allowed {
		return err
	}
	token, session, err := a.auth.Verify(r.Context(), in.Email, in.Code, in.Reactivate, !adminOnly)
	if err != nil {
		return err
	}
	if adminOnly && !session.User.IsAdmin {
		_ = a.auth.Logout(r.Context(), token)
		return domain.ErrForbidden
	}
	a.setSession(w, token, session.ExpiresAt)
	// Keep the guest token valid for other open login tabs; it grants no session.

	respond(w, 200, map[string]any{"user": session.User, "email": session.User.Email, "csrf": session.CSRF})
	return nil
}
func (a *API) logout(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	cookie, _ := r.Cookie(a.cfg.SessionCookie())
	if err := a.auth.Logout(r.Context(), cookie.Value); err != nil {
		return err
	}
	a.clearSession(w)
	respond(w, 200, true)
	return nil
}
func (a *API) me(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	respond(w, 200, map[string]any{"user": s.User, "email": s.User.Email, "csrf": s.CSRF})
	return nil
}
func (a *API) updateMe(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	var in users.Update
	if err := decode(w, r, &in); err != nil {
		return err
	}
	user, err := a.users.Update(r.Context(), s.User.ID, in)
	if err != nil {
		return err
	}
	respond(w, 200, user)
	return nil
}
func (a *API) requestDeletionCode(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	cookie, _ := r.Cookie(a.cfg.SessionCookie())
	if err := a.auth.RequestDeletion(r.Context(), s.User.ID, cookie.Value); err != nil {
		return err
	}
	respond(w, 200, map[string]bool{"sent": true})
	return nil
}
func (a *API) deleteAccount(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	cookie, _ := r.Cookie(a.cfg.SessionCookie())
	if err := a.auth.DeleteAccount(r.Context(), s.User.ID, cookie.Value, in.Code); err != nil {
		return err
	}
	a.clearSession(w)
	pending := false
	if err := a.media.FlushDeletions(r.Context()); err != nil {
		pending = true
		a.logger.Error("file cleanup scheduled for retry")
	}
	respond(w, 200, map[string]bool{"deleted": true, "files_pending": pending})
	return nil
}
func (a *API) profile(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	u, err := a.users.GetForViewer(r.Context(), id, s.User.ID, s.User.IsAdmin)
	if err != nil {
		return err
	}
	data := map[string]any{"id": u.ID, "nickname": u.Nickname, "created_at": u.CreatedAt, "stars": u.Stars, "starred": u.Starred}
	if s.User.IsAdmin {
		data["active"] = u.Active
		data["blocked"] = u.Blocked
	}
	respond(w, 200, data)
	return nil
}
func (a *API) catalog(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	c, err := a.locations.Catalog(r.Context())
	if err != nil {
		return err
	}
	respond(w, 200, c)
	return nil
}
func (a *API) feed(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	f, err := filter(r)
	if err != nil {
		return err
	}
	page, err := a.listings.ListForViewer(r.Context(), s.User.ID, f, s.User.IsAdmin)
	if err != nil {
		return err
	}
	respond(w, 200, page)
	return nil
}
func (a *API) getListing(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	l, err := a.listings.Get(r.Context(), id, s.User.ID, s.User.IsAdmin)
	if err != nil {
		return err
	}
	respond(w, 200, l)
	return nil
}
func (a *API) createListing(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	key := r.Header.Get("Idempotency-Key")
	if err := idempotency.Validate(key); err != nil {
		return err
	}
	var in listings.Input
	if err := decode(w, r, &in); err != nil {
		return err
	}
	l, err := a.listings.CreateOnce(r.Context(), s.User.ID, in, key)
	if err != nil {
		return err
	}
	respond(w, 201, l)
	return nil
}
func (a *API) updateListing(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	var in listings.Input
	if err = decode(w, r, &in); err != nil {
		return err
	}
	l, err := a.listings.Update(r.Context(), id, s.User.ID, in)
	if err != nil {
		return err
	}
	respond(w, 200, l)
	return nil
}
func (a *API) transition(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	var in struct {
		Action  string `json:"action"`
		Outcome string `json:"outcome"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	l, err := a.listings.TransitionWithOutcome(r.Context(), id, s.User.ID, in.Action, in.Outcome)
	if err != nil {
		return err
	}
	respond(w, 200, l)
	return nil
}
func (a *API) favorite(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	if err = a.favorites.Set(r.Context(), s.User.ID, id, r.Method == "PUT"); err != nil {
		return err
	}
	respond(w, 200, true)
	return nil
}
func (a *API) star(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	if err = a.stars.Set(r.Context(), s.User.ID, id, r.Method == "PUT"); err != nil {
		return err
	}
	u, err := a.users.Get(r.Context(), id, s.User.ID)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"stars": u.Stars, "starred": u.Starred})
	return nil
}
func (a *API) uploadPhoto(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	key := r.Header.Get("Idempotency-Key")
	if err = idempotency.Validate(key); err != nil {
		return err
	}
	read := func() ([]byte, error) {
		r.Body = http.MaxBytesReader(w, r.Body, media.MaxFileSize+1024*1024)
		if err := r.ParseMultipartForm(media.MaxFileSize); err != nil {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
			return nil, domain.Invalid("Не удалось прочитать фотографию: лимит 5 МБ")
		}
		defer r.MultipartForm.RemoveAll()
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, domain.Invalid("Выберите фотографию")
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, media.MaxFileSize+1))
	}
	var photo listings.Photo
	if r.Method == http.MethodPut {
		photo, err = a.media.ReplaceFrom(r.Context(), id, s.User.ID, r.PathValue("photo"), key, read)
	} else {
		photo, err = a.media.UploadFrom(r.Context(), id, s.User.ID, key, read)
	}
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if r.Method == http.MethodPut {
		status = http.StatusOK
	}
	respond(w, status, photo)
	return nil
}
func (a *API) deletePhoto(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	if err = a.media.Delete(r.Context(), r.PathValue("photo"), id, s.User.ID); err != nil {
		return err
	}
	respond(w, 200, true)
	return nil
}
func (a *API) reorderPhotos(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	if err = a.media.Reorder(r.Context(), id, s.User.ID, in.IDs); err != nil {
		return err
	}
	respond(w, 200, true)
	return nil
}
func (a *API) getPhoto(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	file, created, err := a.media.Open(r.Context(), r.PathValue("photo"), s.User.ID, s.User.IsAdmin)
	if err != nil {
		return err
	}
	defer file.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "photo.jpg", created, file)
	return nil
}
func (a *API) report(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	var in struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Reason     string `json:"reason"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	id, err := a.moderation.Report(r.Context(), s.User.ID, in.TargetType, in.TargetID, in.Reason)
	if err != nil {
		return err
	}
	respond(w, 201, map[string]int64{"id": id})
	return nil
}
func (a *API) reports(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	offset, err := integer(r, "offset", 0)
	if err != nil {
		return err
	}
	items, err := a.moderation.Reports(r.Context(), r.URL.Query().Get("status"), offset)
	if err != nil {
		return err
	}
	respond(w, 200, items)
	return nil
}
func (a *API) resolveReport(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	var in struct {
		Status     string `json:"status"`
		Resolution string `json:"resolution"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	if err = a.moderation.Resolve(r.Context(), id, s.User.ID, in.Status, in.Resolution); err != nil {
		return err
	}
	respond(w, 200, true)
	return nil
}
func (a *API) accounts(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	offset, err := integer(r, "offset", 0)
	if err != nil {
		return err
	}
	items, err := a.moderation.Accounts(r.Context(), offset)
	if err != nil {
		return err
	}
	respond(w, 200, items)
	return nil
}
func (a *API) moderate(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	var in struct {
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Action     string `json:"action"`
		Reason     string `json:"reason"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	err := a.moderation.Action(r.Context(), s.User.ID, in.TargetType, in.TargetID, in.Action, in.Reason)
	if err != nil {
		return err
	}
	if err = a.media.FlushDeletions(r.Context()); err != nil {
		a.logger.Error("file cleanup scheduled for retry")
	}
	respond(w, 200, true)
	return nil
}
func (a *API) createDorm(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	var in struct {
		Location string `json:"location"`
		Name     string `json:"name"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	d, err := a.locations.CreateDorm(r.Context(), in.Location, in.Name)
	if err != nil {
		return err
	}
	respond(w, 201, d)
	return nil
}
func (a *API) deleteDorm(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	if err = a.locations.DeleteDorm(r.Context(), id); err != nil {
		return err
	}
	respond(w, 200, true)
	return nil
}

func (a *API) reviewQueue(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	f, err := filter(r)
	if err != nil {
		return err
	}
	f.Scope = "review"
	f.Status = ""
	f.Limit = 24
	page, err := a.listings.ListForViewer(r.Context(), s.User.ID, f, true)
	if err != nil {
		return err
	}
	respond(w, 200, page)
	return nil
}
func (a *API) reviewListing(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	var in struct {
		Action   string `json:"action"`
		Revision int64  `json:"revision"`
		Comment  string `json:"comment"`
	}
	if err = decode(w, r, &in); err != nil {
		return err
	}
	if in.Action != "approve" && in.Action != "reject" {
		return domain.Invalid("Выберите одобрение или отказ")
	}
	l, err := a.listings.Review(r.Context(), id, s.User.ID, in.Revision, in.Action == "approve", in.Comment)
	if err != nil {
		return err
	}
	respond(w, 200, l)
	return nil
}
func (a *API) deleteListing(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	id, err := positiveID(r)
	if err != nil {
		return err
	}
	if err = a.listings.Delete(r.Context(), id, s.User.ID); err != nil {
		return err
	}
	if err = a.media.FlushDeletions(r.Context()); err != nil {
		a.logger.Error("file cleanup scheduled for retry")
	}
	respond(w, 200, true)
	return nil
}
func (a *API) statistics(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	total, err := a.listings.CompletedDeals(r.Context())
	if err != nil {
		return err
	}
	respond(w, 200, map[string]int64{"completed_deals": total})
	return nil
}
