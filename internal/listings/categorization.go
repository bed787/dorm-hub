package listings

import (
	"context"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/locations"
	"dorm-hub/internal/platform/postgres"
)

// A moderator can classify an ad without changing its text/publication state.
func (s *Service) SetCategory(ctx context.Context, id, actor, revision int64, category string) (Listing, error) {
	if !locations.ValidCategory(category) {
		return Listing{}, domain.Invalid("Выберите категорию")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Listing{}, err
	}
	defer tx.Rollback(context.Background())
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active AND NOT blocked AND is_admin)`, actor).Scan(&allowed); err != nil {
		return Listing{}, err
	}
	if !allowed {
		return Listing{}, domain.ErrForbidden
	}
	var owner int64
	if err = tx.QueryRow(ctx, `SELECT author_id FROM listings WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&owner); err != nil {
		return Listing{}, postgres.Error(err)
	}
	// Use the same owner/listing lock order as editing, deletion and moderation.
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, owner); err != nil {
		return Listing{}, err
	}
	var current int64
	if err = tx.QueryRow(ctx, `SELECT revision FROM listings WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&current); err != nil {
		return Listing{}, postgres.Error(err)
	}
	if revision < 1 || current != revision {
		return Listing{}, domain.Conflict("Объявление изменилось. Обновите страницу")
	}
	if _, err = tx.Exec(ctx, `UPDATE listings SET category=$2,category_needs_review=false,revision=revision+1,updated_at=now() WHERE id=$1`, id, category); err != nil {
		return Listing{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO moderation_actions(actor_id,target_type,target_id,action,reason) VALUES($1,'listing',$2,'category',$3)`, actor, id, category); err != nil {
		return Listing{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Listing{}, err
	}
	return s.Get(ctx, id, actor, true)
}
