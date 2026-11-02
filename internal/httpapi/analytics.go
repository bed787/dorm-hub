package httpapi

import (
	"context"
	"dorm-hub/internal/analytics"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"net/http"
	"time"
)

func meaningfulActivity(pattern string) bool {
	switch pattern {
	case "GET /api/listings", "GET /api/showcase", "GET /api/listings/{id}", "POST /api/listings", "PUT /api/listings/{id}", "POST /api/listings/{id}/status", "POST /api/analytics/events":
		return true
	}
	return false
}
func (a *API) recordActivity(ctx context.Context, user int64) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := a.analytics.Active(ctx, user); err != nil {
		a.metrics.AnalyticsErrors.Inc()
	}
}
func (a *API) recordView(ctx context.Context, user, listing int64) {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := a.analytics.Event(ctx, user, listing, "view"); err != nil {
		a.metrics.AnalyticsErrors.Inc()
	}
}
func (a *API) analyticsEvent(w http.ResponseWriter, r *http.Request, s auth.Session) error {
	var in struct {
		ListingID int64  `json:"listing_id"`
		Event     string `json:"event"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Event != "telegram" && in.Event != "vk" {
		return domain.Invalid("Неизвестное событие")
	}
	// This optional endpoint never changes listings or authorizes access to them.
	if err := a.analytics.Event(r.Context(), s.User.ID, in.ListingID, in.Event); err != nil {
		return err
	}
	respond(w, http.StatusOK, map[string]bool{"accepted": true})
	return nil
}
func (a *API) analyticsSummary(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	days, err := integer(r, "days", 7)
	if err != nil {
		return err
	}
	offset, err := integer(r, "offset", 0)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	page, err := a.analytics.Summary(ctx, days, offset)
	if err != nil {
		return err
	}
	respond(w, 200, struct {
		analytics.Summary
		MonitoringEnabled bool `json:"monitoring_enabled"`
	}{page, a.cfg.MonitoringURL != ""})
	return nil
}
