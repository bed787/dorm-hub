package users

import (
	"dorm-hub/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }
func ValidateContacts(telegram, vk string, required bool) error {
	if required && telegram == "" && vk == "" {
		return domain.Invalid("Укажите Telegram или VK")
	}
	for _, c := range []struct {
		value string
		hosts []string
	}{{telegram, []string{"t.me", "telegram.me"}}, {vk, []string{"vk.com", "vk.ru"}}} {
		if c.value == "" {
			continue
		}
		if len(c.value) > 300 {
			return domain.Invalid("Ссылка слишком длинная")
		}
		u, err := url.Parse(c.value)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || strings.Trim(u.Path, "/") == "" || u.Fragment != "" {
			return domain.Invalid("Контакты должны быть HTTPS-ссылками на Telegram или VK")
		}
		valid := false
		for _, h := range c.hosts {
			if strings.EqualFold(u.Host, h) {
				valid = true
			}
		}
		if !valid {
			return domain.Invalid("Неверный домен контактной ссылки")
		}
	}
	return nil
}
func validateUpdate(in Update) error {
	n := utf8.RuneCountInString(in.Nickname)
	if n < 3 || n > 30 {
		return domain.Invalid("Никнейм должен содержать от 3 до 30 символов")
	}
	for _, r := range in.Nickname {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != ' ' {
			return domain.Invalid("В никнейме допустимы буквы, цифры, пробел, дефис и подчёркивание")
		}
	}
	return ValidateContacts(in.Telegram, in.VK, false)
}

// Default nicknames are allocated from permanent user IDs. Reserving this
// namespace prevents a rename from blocking another student's registration.
func reservedNickname(nickname string) bool {
	const prefix = "студент_"
	value := strings.ToLower(nickname)
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(value, prefix)
	if suffix == "" {
		return false
	}
	for _, digit := range suffix {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// Accept a full HTTPS link or the username users normally copy from a profile.
// Validation remains separate, so unfinished draft text can still be retained.
func NormalizeContact(value, host string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	for _, prefix := range []string{"t.me/", "telegram.me/", "vk.com/", "vk.ru/"} {
		if strings.HasPrefix(lower, prefix) {
			return "https://" + value
		}
	}
	username := strings.TrimPrefix(value, "@")
	if username != "" && !strings.ContainsAny(username, "/:?#\\ @") {
		simple := true
		for _, r := range username {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
				simple = false
				break
			}
		}
		if simple {
			return "https://" + host + "/" + username
		}
	}
	return value
}
