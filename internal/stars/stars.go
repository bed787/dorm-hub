package stars

import (
	"context"
	"dorm-hub/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db} }
func (s *Service) Set(ctx context.Context, from, to int64, value bool) error {
	if from == to {
		return domain.Invalid("Нельзя поставить звезду самому себе")
	}
	if !value {
		_, err := s.db.Exec(ctx, `DELETE FROM stars WHERE from_user=$1 AND to_user=$2`, from, to)
		return err
	}
	tag, err := s.db.Exec(ctx, `INSERT INTO stars(from_user,to_user) SELECT $1,id FROM users WHERE id=$2 AND active AND NOT blocked ON CONFLICT DO NOTHING`, from, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stars WHERE from_user=$1 AND to_user=$2)`, from, to).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
	}
	return nil
}
