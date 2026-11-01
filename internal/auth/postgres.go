package auth

import (
	"context"
	"crypto/hmac"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/platform/postgres"
	"dorm-hub/internal/users"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) saveCode(ctx context.Context, email, digest string, acceptance []byte) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,17))`, email); err != nil {
		return err
	}
	var sent time.Time
	err = tx.QueryRow(ctx, `SELECT created_at FROM login_codes WHERE email=$1`, email).Scan(&sent)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && time.Since(sent) < time.Minute {
		return &domain.Error{Kind: domain.ErrRateLimit, Message: "Повторный код можно запросить через минуту"}
	}
	var document any
	if len(acceptance) != 0 {
		document = string(acceptance)
	}
	_, err = tx.Exec(ctx, `INSERT INTO login_codes(email,digest,expires_at,legal_acceptance) VALUES($1,$2,now()+interval '10 minutes',$3::jsonb) ON CONFLICT(email) DO UPDATE SET digest=excluded.digest,created_at=now(),expires_at=excluded.expires_at,attempts=0,legal_acceptance=excluded.legal_acceptance`, email, digest, document)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) Verify(ctx context.Context, email, code string, reactivate bool, requireAcceptance ...bool) (string, Session, error) {
	email, err := s.normalize(email)
	if err != nil {
		return "", Session{}, err
	}
	if len(code) != 6 {
		return "", Session{}, domain.Invalid("Код должен состоять из шести цифр")
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return "", Session{}, domain.Invalid("Код должен состоять из шести цифр")
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", Session{}, err
	}
	defer tx.Rollback(context.Background())
	var existing int64
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", Session{}, err
	}
	var digest string
	var expires time.Time
	var attempts int
	var rawAcceptance []byte
	err = tx.QueryRow(ctx, `SELECT digest,expires_at,attempts,legal_acceptance FROM login_codes WHERE email=$1 FOR UPDATE`, email).Scan(&digest, &expires, &attempts, &rawAcceptance)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", Session{}, domain.Invalid("Код неверен или истёк")
	}
	if err != nil {
		return "", Session{}, err
	}
	if attempts >= 5 || !time.Now().Before(expires) {
		return "", Session{}, domain.Invalid("Код неверен или истёк")
	}
	if !hmac.Equal([]byte(digest), []byte(s.digest(email, code))) {
		if _, err = tx.Exec(ctx, `UPDATE login_codes SET attempts=attempts+1 WHERE email=$1`, email); err != nil {
			return "", Session{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return "", Session{}, err
		}
		return "", Session{}, domain.Invalid("Код неверен или истёк")
	}
	var acceptance Acceptance
	if len(rawAcceptance) != 0 {
		if err = json.Unmarshal(rawAcceptance, &acceptance); err != nil {
			return "", Session{}, err
		}
	}
	if len(requireAcceptance) > 0 && requireAcceptance[0] && !acceptance.valid() {
		return "", Session{}, domain.Invalid("Запросите новый код, подтвердив согласие и правила")
	}

	seed, err := Token()
	if err != nil {
		return "", Session{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO users(email,nickname,is_admin) VALUES($1,$2,$3) ON CONFLICT(email) DO NOTHING`, email, "new_"+seed, s.cfg.IsAdmin(email))
	if err != nil {
		return "", Session{}, err
	}
	var u users.User
	err = tx.QueryRow(ctx, `SELECT id,email,nickname,nickname_changed,telegram,vk,active,blocked,is_admin,created_at FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&u.ID, &u.Email, &u.Nickname, &u.NicknameChanged, &u.Telegram, &u.VK, &u.Active, &u.Blocked, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		return "", Session{}, err
	}
	if u.Blocked {
		return "", Session{}, domain.Forbidden("Аккаунт заблокирован")
	}
	if acceptance.valid() {
		_, err = tx.Exec(ctx, `INSERT INTO user_legal_acceptances(user_id,consent_version,rules_version,consent_text,rules_text,accepted_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, u.ID, acceptance.ConsentVersion, acceptance.RulesVersion, acceptance.ConsentText, acceptance.RulesText, acceptance.AcceptedAt)
		if err != nil {
			return "", Session{}, err
		}
	}

	if !u.NicknameChanged {
		u.Nickname = "Студент_" + formatID(u.ID)
	}
	u.Active = true
	u.IsAdmin = u.IsAdmin || s.cfg.IsAdmin(email)
	if _, err = tx.Exec(ctx, `UPDATE users SET nickname=$2,active=true,is_admin=$3 WHERE id=$1`, u.ID, u.Nickname, u.IsAdmin); err != nil {
		return "", Session{}, err
	}
	token, err := Token()
	if err != nil {
		return "", Session{}, err
	}
	csrf, err := Token()
	if err != nil {
		return "", Session{}, err
	}
	expiry := time.Now().Add(30 * 24 * time.Hour)
	if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND expires_at<now()`, u.ID); err != nil {
		return "", Session{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES($1,$2,$3,$4)`, Hash(token), u.ID, csrf, expiry); err != nil {
		return "", Session{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM login_codes WHERE email=$1`, email); err != nil {
		return "", Session{}, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM stars WHERE to_user=$1`, u.ID).Scan(&u.Stars); err != nil {
		return "", Session{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", Session{}, postgres.Error(err)
	}
	return token, Session{User: u, CSRF: csrf, ExpiresAt: expiry}, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if len(token) != 64 {
		return Session{}, domain.ErrUnauthorized
	}
	var v Session
	err := s.db.QueryRow(ctx, `SELECT u.id,u.email,u.nickname,u.nickname_changed,u.telegram,u.vk,u.active,u.blocked,u.is_admin,u.created_at,s.csrf,s.expires_at,(SELECT count(*) FROM stars WHERE to_user=u.id) FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND u.active AND NOT u.blocked`, Hash(token)).Scan(&v.User.ID, &v.User.Email, &v.User.Nickname, &v.User.NicknameChanged, &v.User.Telegram, &v.User.VK, &v.User.Active, &v.User.Blocked, &v.User.IsAdmin, &v.User.CreatedAt, &v.CSRF, &v.ExpiresAt, &v.User.Stars)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, domain.ErrUnauthorized
	}
	return v, err
}
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, Hash(token))
	return err
}

func formatID(id int64) string { return strconv.FormatInt(id, 10) }
