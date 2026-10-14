package users

import (
	"context"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/platform/postgres"
	"github.com/jackc/pgx/v5"
	"strings"
)

const columns = `u.id,u.email,u.nickname,u.nickname_changed,u.telegram,u.vk,u.active,u.blocked,u.is_admin,u.created_at,
 (SELECT count(*) FROM stars s WHERE s.to_user=u.id),EXISTS(SELECT 1 FROM stars s WHERE s.from_user=$2 AND s.to_user=u.id)`

func scan(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Nickname, &u.NicknameChanged, &u.Telegram, &u.VK, &u.Active, &u.Blocked, &u.IsAdmin, &u.CreatedAt, &u.Stars, &u.Starred)
	return u, postgres.Error(err)
}
func (s *Service) Get(ctx context.Context, id, viewer int64) (User, error) {
	return s.GetForViewer(ctx, id, viewer, false)
}
func (s *Service) GetForViewer(ctx context.Context, id, viewer int64, admin bool) (User, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM users u WHERE u.id=$1 AND ((u.active AND NOT u.blocked) OR u.id=$2 OR $3)`, id, viewer, admin))
}
func (s *Service) Update(ctx context.Context, id int64, in Update) (User, error) {
	in.Nickname = strings.TrimSpace(in.Nickname)
	in.Telegram = NormalizeContact(in.Telegram, "t.me")
	in.VK = NormalizeContact(in.VK, "vk.com")
	if err := validateUpdate(in); err != nil {
		return User{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	var nickname string
	var changed, active, blocked bool
	if err = tx.QueryRow(ctx, `SELECT nickname,nickname_changed,active,blocked FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&nickname, &changed, &active, &blocked); err != nil {
		return User{}, postgres.Error(err)
	}
	if !active || blocked {
		return User{}, domain.ErrForbidden
	}
	if nickname != in.Nickname && changed {
		return User{}, domain.Conflict("Никнейм уже был изменён один раз")
	}
	if nickname != in.Nickname && reservedNickname(in.Nickname) {
		return User{}, domain.Invalid("Никнеймы Студент_N зарезервированы для новых участников")
	}
	_, err = tx.Exec(ctx, `UPDATE users SET nickname=$2,nickname_changed=nickname_changed OR nickname<>$2,telegram=$3,vk=$4 WHERE id=$1`, id, in.Nickname, in.Telegram, in.VK)
	if err != nil {
		return User{}, postgres.Error(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return s.Get(ctx, id, id)
}

// DeleteInTx must be called while holding the user's row lock. Every reference
// containing user data is removed, including free-text moderation records.
func DeleteInTx(ctx context.Context, tx pgx.Tx, id int64, email string) error {
	for _, query := range []string{
		`INSERT INTO pending_file_deletions(filename) SELECT p.filename FROM photos p JOIN listings l ON l.id=p.listing_id WHERE l.author_id=$1 ON CONFLICT DO NOTHING`,
		`DELETE FROM reports WHERE reporter_id=$1 OR user_id=$1 OR resolved_by=$1 OR (target_type='user' AND target_id=$1) OR listing_id IN (SELECT id FROM listings WHERE author_id=$1) OR (target_type='listing' AND target_id IN (SELECT id FROM listings WHERE author_id=$1))`,
		`DELETE FROM moderation_actions WHERE actor_id=$1 OR (target_type='user' AND target_id=$1) OR (target_type='listing' AND target_id IN (SELECT id FROM listings WHERE author_id=$1))`,
		`DELETE FROM users WHERE id=$1`,
	} {
		if _, err := tx.Exec(ctx, query, id); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `DELETE FROM login_codes WHERE email=$1`, email)
	return err
}
