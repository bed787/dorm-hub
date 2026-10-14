package moderation

import (
	"context"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/platform/postgres"
	"dorm-hub/internal/users"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db} }

type Report struct {
	ID         int64     `json:"id"`
	ReporterID *int64    `json:"reporter_id"`
	TargetType string    `json:"target_type"`
	TargetID   int64     `json:"target_id"`
	Reason     string    `json:"reason"`
	Status     string    `json:"status"`
	Resolution string    `json:"resolution"`
	CreatedAt  time.Time `json:"created_at"`
}
type Account struct {
	ID        int64     `json:"id"`
	Nickname  string    `json:"nickname"`
	Active    bool      `json:"active"`
	Blocked   bool      `json:"blocked"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

func reasonValid(reason string) error {
	if strings.ContainsRune(reason, 0) {
		return domain.Invalid("Текст содержит недопустимый символ")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(reason)); n < 10 || n > 2000 {
		return domain.Invalid("Причина должна содержать от 10 до 2000 символов")
	}
	return nil
}
func (s *Service) Report(ctx context.Context, reporter int64, kind string, target int64, reason string) (int64, error) {
	if err := reasonValid(reason); err != nil {
		return 0, err
	}
	var exists bool
	var listing, user *int64
	switch kind {
	case "listing":
		listing = &target
		err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM listings l JOIN users u ON u.id=l.author_id WHERE l.id=$1 AND l.author_id<>$2 AND NOT l.hidden AND l.deleted_at IS NULL AND u.active AND NOT u.blocked AND l.status='active')`, target, reporter).Scan(&exists)
		if err != nil {
			return 0, err
		}
	case "user":
		user = &target
		err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND id<>$2 AND active AND NOT blocked)`, target, reporter).Scan(&exists)
		if err != nil {
			return 0, err
		}
	default:
		return 0, domain.Invalid("Неверный объект жалобы")
	}
	if !exists {
		return 0, domain.ErrNotFound
	}
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO reports(reporter_id,listing_id,user_id,target_type,target_id,reason) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, reporter, listing, user, kind, target, strings.TrimSpace(reason)).Scan(&id)
	return id, postgres.Error(err)
}
func (s *Service) Reports(ctx context.Context, status string, offset int) ([]Report, error) {
	if status != "" && status != "new" && status != "resolved" && status != "dismissed" {
		return nil, domain.Invalid("Неверный статус жалоб")
	}
	if offset < 0 {
		return nil, domain.Invalid("Неверная страница")
	}
	rows, err := s.db.Query(ctx, `SELECT id,reporter_id,target_type,target_id,reason,status,resolution,created_at FROM reports WHERE ($1='' OR status=$1) ORDER BY created_at DESC LIMIT 50 OFFSET $2`, status, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Report{}
	for rows.Next() {
		var x Report
		if err = rows.Scan(&x.ID, &x.ReporterID, &x.TargetType, &x.TargetID, &x.Reason, &x.Status, &x.Resolution, &x.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}
func (s *Service) Resolve(ctx context.Context, id, actor int64, status, resolution string) error {
	if err := s.requireAdmin(ctx, actor); err != nil {
		return err
	}

	if status != "resolved" && status != "dismissed" {
		return domain.Invalid("Неверное решение")
	}
	if err := reasonValid(resolution); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE reports SET status=$3,resolution=$4,resolved_by=$2,resolved_at=now() WHERE id=$1 AND status='new'`, id, actor, status, strings.TrimSpace(resolution))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.Conflict("Жалоба уже обработана или не найдена")
	}
	return nil
}
func (s *Service) Accounts(ctx context.Context, offset int) ([]Account, error) {
	if offset < 0 {
		return nil, domain.Invalid("Неверная страница")
	}
	rows, err := s.db.Query(ctx, `SELECT id,nickname,active,blocked,is_admin,created_at FROM users ORDER BY id DESC LIMIT 50 OFFSET $1`, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Account{}
	for rows.Next() {
		var x Account
		if err = rows.Scan(&x.ID, &x.Nickname, &x.Active, &x.Blocked, &x.IsAdmin, &x.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}
func (s *Service) Action(ctx context.Context, actor int64, kind string, target int64, action, reason string) ([]string, error) {
	if err := s.requireAdmin(ctx, actor); err != nil {
		return nil, err
	}

	if err := reasonValid(reason); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	files := []string{}
	if kind == "user" {
		if target == actor {
			return nil, domain.Forbidden("Нельзя применить это действие к своему аккаунту")
		}
		var admin bool
		if err = tx.QueryRow(ctx, `SELECT is_admin FROM users WHERE id=$1 FOR UPDATE`, target).Scan(&admin); err != nil {
			return nil, postgres.Error(err)
		}
		if admin {
			return nil, domain.Forbidden("Действия над другим администратором запрещены")
		}
		switch action {
		case "block", "unblock":
			_, err = tx.Exec(ctx, `UPDATE users SET blocked=$2 WHERE id=$1`, target, action == "block")
			if err == nil && action == "block" {
				_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, target)
			}
		case "delete":
			var email string
			if err = tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, target).Scan(&email); err != nil {
				return nil, err
			}
			if err = users.DeleteInTx(ctx, tx, target, email); err != nil {
				return nil, err
			}
			return files, tx.Commit(ctx)
		default:
			return nil, domain.Invalid("Неверное действие над аккаунтом")
		}
	} else if kind == "listing" {
		var author int64
		if err = tx.QueryRow(ctx, `SELECT author_id FROM listings WHERE id=$1 AND deleted_at IS NULL`, target).Scan(&author); err != nil {
			return nil, postgres.Error(err)
		}
		// All write paths lock author first, then listing, in a consistent order.
		if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, author); err != nil {
			return nil, err
		}
		switch action {
		case "hide", "show":
			tag, e := tx.Exec(ctx, `UPDATE listings SET hidden=$2,status=CASE WHEN status='active' THEN 'pending' ELSE status END,revision=revision+1,updated_at=now() WHERE id=$1 AND deleted_at IS NULL`, target, action == "hide")
			err = e
			if e == nil && tag.RowsAffected() == 0 {
				return nil, domain.ErrNotFound
			}
		case "delete":
			_, err = tx.Exec(ctx, `UPDATE listings SET deleted_at=now(),status='archived',updated_at=now() WHERE id=$1`, target)
		default:
			return nil, domain.Invalid("Неверное действие над объявлением")
		}
	} else {
		return nil, domain.Invalid("Неверный объект модерации")
	}
	if err != nil {
		return nil, postgres.Error(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO moderation_actions(actor_id,target_type,target_id,action,reason) VALUES($1,$2,$3,$4,$5)`, actor, kind, target, action, strings.TrimSpace(reason)); err != nil {
		return nil, err
	}
	return files, tx.Commit(ctx)
}

func (s *Service) requireAdmin(ctx context.Context, actor int64) error {
	var allowed bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active AND NOT blocked AND is_admin)`, actor).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return domain.ErrForbidden
	}
	return nil
}
