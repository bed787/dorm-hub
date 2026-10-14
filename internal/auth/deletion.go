package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/platform/postgres"
	"dorm-hub/internal/users"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"math/big"
	"strings"
	"time"
)

type deletionSender interface {
	SendDeletionCode(context.Context, string, string) error
}

func (s *Service) deletionDigest(user int64, code string) string {
	return s.digest(fmt.Sprintf("delete:%d", user), code)
}
func (s *Service) RequestDeletion(ctx context.Context, user int64, token string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var email string
	var active, blocked bool
	if err = tx.QueryRow(ctx, `SELECT email,active,blocked FROM users WHERE id=$1 FOR UPDATE`, user).Scan(&email, &active, &blocked); err != nil {
		return postgres.Error(err)
	}
	if !active || blocked {
		return domain.ErrForbidden
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now())`, user, Hash(token)).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return domain.ErrUnauthorized
	}
	var created time.Time
	err = tx.QueryRow(ctx, `SELECT created_at FROM account_deletion_codes WHERE user_id=$1`, user).Scan(&created)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && time.Since(created) < time.Minute {
		return &domain.Error{Kind: domain.ErrRateLimit, Message: "Повторный код можно запросить через минуту"}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	digest := s.deletionDigest(user, code)
	_, err = tx.Exec(ctx, `INSERT INTO account_deletion_codes(user_id,session_hash,digest,expires_at) VALUES($1,$2,$3,now()+interval '10 minutes') ON CONFLICT(user_id) DO UPDATE SET session_hash=excluded.session_hash,digest=excluded.digest,expires_at=excluded.expires_at,created_at=now(),attempts=0`, user, Hash(token), digest)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	sender, ok := s.mail.(deletionSender)
	if !ok {
		return fmt.Errorf("mail sender does not support account deletion codes")
	}
	if err = sender.SendDeletionCode(ctx, email, code); err != nil {
		_, _ = s.db.Exec(ctx, `DELETE FROM account_deletion_codes WHERE user_id=$1 AND digest=$2`, user, digest)
		return fmt.Errorf("send deletion code: %w", err)
	}
	return nil
}
func (s *Service) DeleteAccount(ctx context.Context, user int64, token, code string) error {
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return domain.Invalid("Введите шестизначный код удаления")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var email string
	if err = tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1 FOR UPDATE`, user).Scan(&email); err != nil {
		return postgres.Error(err)
	}
	var digest, sessionHash string
	var expires time.Time
	var attempts int
	err = tx.QueryRow(ctx, `SELECT digest,session_hash,expires_at,attempts FROM account_deletion_codes WHERE user_id=$1 FOR UPDATE`, user).Scan(&digest, &sessionHash, &expires, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Invalid("Запросите код удаления")
	}
	if err != nil {
		return err
	}
	if attempts >= 5 || !time.Now().Before(expires) {
		return domain.Invalid("Код удаления неверен или истёк")
	}
	if !hmac.Equal([]byte(sessionHash), []byte(Hash(token))) {
		return domain.Invalid("Код запрошен в другой сессии. Запросите новый код здесь")
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now())`, user, sessionHash).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return domain.ErrUnauthorized
	}
	if !hmac.Equal([]byte(digest), []byte(s.deletionDigest(user, code))) {
		if _, err = tx.Exec(ctx, `UPDATE account_deletion_codes SET attempts=attempts+1 WHERE user_id=$1`, user); err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return domain.Invalid("Код удаления неверен или истёк")
	}
	if err = users.DeleteInTx(ctx, tx, user, email); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
