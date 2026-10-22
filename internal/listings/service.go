package listings

import (
	"dorm-hub/internal/domain"
	"dorm-hub/internal/users"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"unicode/utf8"
)

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }
func Validate(in Input) error {
	if err := ValidateDraft(in); err != nil {
		return err
	}
	if in.Category == "" {
		return domain.Invalid("Выберите категорию")
	}
	if in.Location == "" {
		return domain.Invalid("Выберите местоположение")
	}
	if utf8.RuneCountInString(in.Title) < 3 {
		return domain.Invalid("Название: от 3 до 120 символов")
	}
	if utf8.RuneCountInString(in.Description) < 10 {
		return domain.Invalid("Описание: от 10 до 10 000 символов")
	}
	return users.ValidateContacts(in.Telegram, in.VK, true)
}
func ValidateDraft(in Input) error {
	if strings.ContainsRune(in.Title, 0) || strings.ContainsRune(in.Description, 0) {
		return domain.Invalid("Текст содержит недопустимый символ")
	}
	if !oneOf(in.Kind, "offer", "request") {
		return domain.Invalid("Выберите предложение или заявку")
	}
	if in.Category != "" && !oneOf(in.Category, "things", "study", "help", "housing", "other") {
		return domain.Invalid("Выберите категорию")
	}
	if in.Location != "" && !oneOf(in.Location, "VASILEOSTROVSKY", "PETRODVORTSOVY", "NEVSKY", "EXTERNAL") {
		return domain.Invalid("Выберите местоположение")
	}
	if utf8.RuneCountInString(in.Address) > 300 || strings.ContainsRune(in.Address, 0) {
		return domain.Invalid("Адрес: не больше 300 символов без недопустимых знаков")
	}
	if in.Address != "" && in.Location != "EXTERNAL" {
		return domain.Invalid("Свой адрес можно указать вне УНК университета")
	}
	if in.Location == "EXTERNAL" && in.DormID != nil {
		return domain.Invalid("Для адреса вне УНК общежитие не выбирается")
	}
	if utf8.RuneCountInString(in.Title) > 120 {
		return domain.Invalid("Название: не больше 120 символов")
	}
	if utf8.RuneCountInString(in.Description) > 10000 {
		return domain.Invalid("Описание: не больше 10 000 символов")
	}
	if !oneOf(in.PriceMode, "price", "exchange", "unset") {
		return domain.Invalid("Выберите вариант стоимости")
	}
	if in.PriceMode == "price" {
		if in.PriceMinor == nil || *in.PriceMinor < 0 || *in.PriceMinor > 10000000000 {
			return domain.Invalid("Укажите корректную цену")
		}
	} else if in.PriceMinor != nil {
		return domain.Invalid("Сумма допустима только при выборе цены")
	}
	if in.DormID != nil && (*in.DormID <= 0 || in.Location == "") {
		return domain.Invalid("Выберите район для общежития")
	}
	// Drafts retain partial links while typing; published contacts are validated
	// strictly by Validate. Length and NUL restrictions still apply here.
	for _, value := range []string{in.Telegram, in.VK} {
		if len(value) > 300 || strings.ContainsRune(value, 0) {
			return domain.Invalid("Некорректный контакт")
		}
	}
	return nil
}
func HasContent(in Input) bool {
	return strings.TrimSpace(in.Title) != "" || strings.TrimSpace(in.Description) != "" || in.Category != "" || in.Location != "" || in.PriceMinor != nil || in.HasPhotos
}
func clean(in Input) Input {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.Address = strings.TrimSpace(in.Address)
	in.Telegram = users.NormalizeContact(in.Telegram, "t.me")
	in.VK = users.NormalizeContact(in.VK, "vk.com")
	return in
}
func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}
