package favorites

import (
	"context"
	"dorm-hub/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db} }
func (s *Service) Set(ctx context.Context, user, listing int64, value bool) error {
	if !value {
		_, err := s.db.Exec(ctx, `DELETE FROM favorites WHERE user_id=$1 AND listing_id=$2`, user, listing)
		return err
	}
	tag, err := s.db.Exec(ctx, `INSERT INTO favorites(user_id,listing_id) SELECT $1,l.id FROM listings l JOIN users u ON u.id=l.author_id WHERE l.id=$2 AND l.author_id<>$1 AND NOT l.hidden AND l.deleted_at IS NULL AND u.active AND NOT u.blocked AND l.status='active' ON CONFLICT DO NOTHING`, user, listing)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		err = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM favorites WHERE user_id=$1 AND listing_id=$2)`, user, listing).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return domain.Invalid("В избранное можно добавить только доступное чужое объявление")
		}
	}
	return nil
}
