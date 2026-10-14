package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"dorm-hub/internal/config"
	"dorm-hub/internal/domain"
	sender "dorm-hub/internal/platform/mail"
	"dorm-hub/internal/users"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"math/big"
	"net/mail"
	"strings"
	"time"
)

type Service struct {
	db   *pgxpool.Pool
	cfg  config.Config
	mail sender.Sender
}
type Session struct {
	User      users.User
	CSRF      string
	ExpiresAt time.Time
}

func New(db *pgxpool.Pool, cfg config.Config, mail sender.Sender) *Service {
	return &Service{db: db, cfg: cfg, mail: mail}
}
func Token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func Hash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func (s *Service) normalize(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || !s.cfg.AllowsEmail(email) {
		return "", domain.Invalid("Используйте разрешённую корпоративную почту")
	}
	return email, nil
}
func (s *Service) digest(email, code string) string {
	m := hmac.New(sha256.New, []byte(s.cfg.AuthSecret))
	m.Write([]byte(email + ":" + code))
	return hex.EncodeToString(m.Sum(nil))
}
func (s *Service) Request(ctx context.Context, email string) error {
	email, err := s.normalize(email)
	if err != nil {
		return err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	if err = s.saveCode(ctx, email, s.digest(email, code)); err != nil {
		return err
	}
	if err = s.mail.SendCode(ctx, email, code); err != nil {
		// Delete only this reservation: a newer request must not be invalidated.
		_, _ = s.db.Exec(ctx, `DELETE FROM login_codes WHERE email=$1 AND digest=$2`, email, s.digest(email, code))
		return fmt.Errorf("send login email: %w", err)
	}
	return nil
}

func ValidToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
