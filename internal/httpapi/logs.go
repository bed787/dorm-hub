package httpapi

import (
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (a *API) applicationLogs(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	level := strings.ToUpper(r.URL.Query().Get("level"))
	if level != "" && level != "INFO" && level != "WARN" && level != "ERROR" && level != "DEBUG" {
		return domain.Invalid("Неизвестный уровень журнала")
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if !utf8.ValidString(q) || utf8.RuneCountInString(q) > 200 {
		return domain.Invalid("Поиск: не более 200 символов")
	}
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	respond(w, http.StatusOK, a.logs.Read(level, q))
	return nil
}
