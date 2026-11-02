package analytics

import (
	"context"
	"dorm-hub/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

const today = `(now() AT TIME ZONE 'Europe/Moscow')::date`

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{db: db} }

// Store only the user/day necessary for distinct activity, not a browsing log.
func (s *Service) Active(ctx context.Context, user int64) error {
	_, err := s.db.Exec(ctx, `INSERT INTO analytics_activity(day,user_id)
 SELECT `+today+`,id FROM users WHERE id=$1 AND active AND NOT blocked AND NOT is_admin
 ON CONFLICT DO NOTHING`, user)
	return err
}
func (s *Service) Event(ctx context.Context, user, listing int64, event string) error {
	if event != "view" && event != "telegram" && event != "vk" {
		return domain.Invalid("Неизвестное событие")
	}
	if listing <= 0 {
		return domain.Invalid("Неверное объявление")
	}
	_, err := s.db.Exec(ctx, `INSERT INTO analytics_listing_daily(day,user_id,listing_id,event)
 SELECT `+today+`,$1,l.id,$3 FROM listings l JOIN users owner ON owner.id=l.author_id
 JOIN users viewer ON viewer.id=$1
 WHERE l.id=$2 AND l.author_id<>$1 AND l.deleted_at IS NULL AND NOT l.hidden
 AND l.status='active' AND owner.active AND NOT owner.blocked
 AND viewer.active AND NOT viewer.blocked AND NOT viewer.is_admin
 AND ($3='view' OR ($3='telegram' AND l.telegram<>'') OR ($3='vk' AND l.vk<>''))
 ON CONFLICT DO NOTHING`, user, listing, event)
	return err
}
func (s *Service) Prune(ctx context.Context) error {
	for _, table := range []string{"analytics_activity", "analytics_listing_daily"} {
		if _, err := s.db.Exec(ctx, `DELETE FROM `+table+` WHERE day<`+today+`-89`); err != nil {
			return err
		}
	}
	return nil
}

type Day struct {
	Date         string `json:"date"`
	ActiveUsers  int    `json:"active_users"`
	Publications int    `json:"publications"`
	Views        int    `json:"views"`
	Contacts     int    `json:"contacts"`
	Successful   int    `json:"successful"`
}
type Listing struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Views    int    `json:"views"`
	Contacts int    `json:"contacts"`
}
type Summary struct {
	Days                 int       `json:"days"`
	Timezone             string    `json:"timezone"`
	StartedAt            time.Time `json:"started_at"`
	DAU                  int       `json:"dau"`
	WAU                  int       `json:"wau"`
	MAU                  int       `json:"mau"`
	Publications         int       `json:"publications"`
	Views                int       `json:"views"`
	Contacts             int       `json:"contacts"`
	Successful           int       `json:"successful"`
	Pending              int       `json:"pending"`
	OldestPendingSeconds int64     `json:"oldest_pending_seconds"`
	Published            int       `json:"published"`
	NewUsers             int       `json:"new_users"`
	TotalListings        int       `json:"total_listings"`
	Offset               int       `json:"offset"`
	Limit                int       `json:"limit"`
	Series               []Day     `json:"series"`
	Listings             []Listing `json:"listings"`
}

func (s *Service) Summary(ctx context.Context, days, offset int) (Summary, error) {
	out := Summary{Days: days, Timezone: "Europe/Moscow", Offset: offset, Limit: 20, Series: []Day{}, Listings: []Listing{}}
	if (days != 1 && days != 7 && days != 30) || offset < 0 || offset > 100000 {
		return out, domain.Invalid("Неверный период или страница")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.Background())
	// One consistent snapshot for all dashboard numbers.
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, `SELECT started_at FROM analytics_settings WHERE singleton`).Scan(&out.StartedAt); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT
 count(DISTINCT a.user_id) FILTER(WHERE a.day=`+today+`),
 count(DISTINCT a.user_id) FILTER(WHERE a.day>=`+today+`-6),count(DISTINCT a.user_id)
 FROM analytics_activity a JOIN users u ON u.id=a.user_id
 WHERE a.day BETWEEN `+today+`-29 AND `+today+` AND NOT u.is_admin`).Scan(&out.DAU, &out.WAU, &out.MAU)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT count(*),coalesce(extract(epoch FROM now()-min(l.updated_at)),0)::bigint
 FROM listings l JOIN users u ON u.id=l.author_id WHERE l.status='pending'
 AND l.deleted_at IS NULL AND NOT l.hidden AND u.active AND NOT u.blocked`).Scan(&out.Pending, &out.OldestPendingSeconds)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM listings l JOIN users u ON u.id=l.author_id
 WHERE l.status='active' AND l.deleted_at IS NULL AND NOT l.hidden AND u.active AND NOT u.blocked`).Scan(&out.Published)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE NOT is_admin
 AND (created_at AT TIME ZONE 'Europe/Moscow')::date BETWEEN `+today+`-($1-1) AND `+today, days).Scan(&out.NewUsers)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `WITH days AS (SELECT generate_series((`+today+`-($1-1))::timestamp,`+today+`::timestamp,'1 day')::date AS day),
 a AS (SELECT day,count(*) AS n FROM analytics_activity a JOIN users u ON u.id=a.user_id WHERE NOT u.is_admin GROUP BY day),
 e AS (SELECT day,count(*) FILTER(WHERE event='view') AS views,count(*) FILTER(WHERE event<>'view') AS contacts
 FROM analytics_listing_daily e JOIN users u ON u.id=e.user_id WHERE NOT u.is_admin GROUP BY day),
 p AS (SELECT (first_published_at AT TIME ZONE 'Europe/Moscow')::date AS day,count(*) AS n
 FROM listings l JOIN users u ON u.id=l.author_id WHERE l.deleted_at IS NULL GROUP BY day),
 c AS (SELECT (successful_at AT TIME ZONE 'Europe/Moscow')::date AS day,count(*) AS n
 FROM listings l JOIN users u ON u.id=l.author_id WHERE l.deleted_at IS NULL GROUP BY day)
 SELECT d.day::text,coalesce(a.n,0),coalesce(p.n,0),coalesce(e.views,0),coalesce(e.contacts,0),coalesce(c.n,0)
 FROM days d LEFT JOIN a USING(day) LEFT JOIN e USING(day) LEFT JOIN p USING(day) LEFT JOIN c USING(day) ORDER BY d.day`, days)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var d Day
		if err = rows.Scan(&d.Date, &d.ActiveUsers, &d.Publications, &d.Views, &d.Contacts, &d.Successful); err != nil {
			rows.Close()
			return out, err
		}
		out.Series = append(out.Series, d)
		out.Publications += d.Publications
		out.Views += d.Views
		out.Contacts += d.Contacts
		out.Successful += d.Successful
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT count(DISTINCT listing_id) FROM analytics_listing_daily e
 JOIN users u ON u.id=e.user_id WHERE NOT u.is_admin AND e.day BETWEEN `+today+`-($1-1) AND `+today, days).Scan(&out.TotalListings)
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT l.id,l.title,coalesce(l.category,''),count(*) FILTER(WHERE e.event='view'),count(*) FILTER(WHERE e.event<>'view')
 FROM analytics_listing_daily e JOIN listings l ON l.id=e.listing_id JOIN users u ON u.id=e.user_id
 WHERE NOT u.is_admin AND e.day BETWEEN `+today+`-($1-1) AND `+today+`
 GROUP BY l.id ORDER BY count(*) DESC,l.id DESC LIMIT 20 OFFSET $2`, days, offset)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var l Listing
		if err = rows.Scan(&l.ID, &l.Title, &l.Category, &l.Views, &l.Contacts); err != nil {
			rows.Close()
			return out, err
		}
		out.Listings = append(out.Listings, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
