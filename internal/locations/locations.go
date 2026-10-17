package locations

import (
	"context"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/platform/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"unicode/utf8"
)

type Dorm struct {
	ID       int64  `json:"id"`
	Location string `json:"location"`
	Name     string `json:"name"`
}
type Item struct {
	Code string `json:"code"`
	Name string `json:"name"`
}
type Catalog struct {
	Locations  []Item `json:"locations"`
	Categories []Item `json:"categories"`
	Dorms      []Dorm `json:"dorms"`
}
type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db} }
func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	c := Catalog{Locations: []Item{}, Categories: []Item{}, Dorms: []Dorm{}}
	for _, source := range []struct {
		query  string
		target *[]Item
	}{{`SELECT code,name FROM locations WHERE code IN ('VASILEOSTROVSKY','PETRODVORTSOVY','NEVSKY') ORDER BY CASE code WHEN 'VASILEOSTROVSKY' THEN 1 WHEN 'PETRODVORTSOVY' THEN 2 ELSE 3 END`, &c.Locations}, {`SELECT code,name FROM categories ORDER BY CASE code WHEN 'things' THEN 1 WHEN 'study' THEN 2 WHEN 'help' THEN 3 WHEN 'housing' THEN 4 ELSE 5 END`, &c.Categories}} {
		rows, err := s.db.Query(ctx, source.query)
		if err != nil {
			return c, err
		}
		for rows.Next() {
			var x Item
			if err = rows.Scan(&x.Code, &x.Name); err != nil {
				rows.Close()
				return c, err
			}
			*source.target = append(*source.target, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return c, err
		}
	}
	rows, err := s.db.Query(ctx, `SELECT id,location,name FROM dorms ORDER BY location,substring(name from '^№ ([0-9]+)')::numeric NULLS LAST,name`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Dorm
		if err = rows.Scan(&d.ID, &d.Location, &d.Name); err != nil {
			return c, err
		}
		c.Dorms = append(c.Dorms, d)
	}
	return c, rows.Err()
}
func (s *Service) CreateDorm(ctx context.Context, location, name string) (Dorm, error) {
	if location != "VASILEOSTROVSKY" && location != "PETRODVORTSOVY" && location != "NEVSKY" {
		return Dorm{}, domain.Invalid("Выберите район")
	}
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return Dorm{}, domain.Invalid("Название общежития: от 1 до 100 символов")
	}
	var d Dorm
	err := s.db.QueryRow(ctx, `INSERT INTO dorms(location,name) VALUES($1,$2) RETURNING id,location,name`, location, name).Scan(&d.ID, &d.Location, &d.Name)
	return d, postgres.Error(err)
}
func (s *Service) DeleteDorm(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM dorms WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
