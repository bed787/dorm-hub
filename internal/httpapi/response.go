package httpapi

import (
	"dorm-hub/internal/domain"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
)

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func decode(w http.ResponseWriter, r *http.Request, target any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return domain.Invalid("Ожидается JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return domain.Invalid("Некорректный JSON или слишком большой запрос")
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return domain.Invalid("В запросе должен быть один JSON-объект")
	}
	return nil
}
func positiveID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, domain.Invalid("Некорректный ID")
	}
	return id, nil
}
func integer(r *http.Request, key string, fallback int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 10000000 {
		return 0, domain.Invalid("Некорректный параметр " + key)
	}
	return n, nil
}
func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := 500, "internal", "Внутренняя ошибка. Попробуйте позже"
	switch {
	case errors.Is(err, domain.ErrInvalid):
		status, code, message = 400, "invalid", err.Error()
	case errors.Is(err, domain.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "Войдите в аккаунт"
	case errors.Is(err, domain.ErrCSRF):
		status, code, message = 403, "csrf", "Обновите страницу входа"
	case errors.Is(err, domain.ErrForbidden):
		status, code, message = 403, "forbidden", "Недостаточно прав"
	case errors.Is(err, domain.ErrNotFound):
		status, code, message = 404, "not_found", "Объект не найден"
	case errors.Is(err, domain.ErrConflict):
		status, code, message = 409, "conflict", err.Error()
	case errors.Is(err, domain.ErrRateLimit):
		status, code, message = 429, "rate_limit", "Слишком много попыток. Попробуйте позже"
		w.Header().Set("Retry-After", "60")
	}
	var public *domain.Error
	if errors.As(err, &public) {
		message = public.Message
	}
	if status == 500 {
		a.logger.Error("api error", "request_id", w.Header().Get("X-Request-ID"), "error_type", fmt.Sprintf("%T", err))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
