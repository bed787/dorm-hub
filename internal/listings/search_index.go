package listings

import (
	"context"
	"dorm-hub/internal/platform/search"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"time"
)

type Searcher interface {
	Search(context.Context, string) ([]search.Hit, error)
}

func NewWithSearch(db *pgxpool.Pool, engine Searcher) *Service {
	return &Service{db: db, search: engine}
}

// SearchSync serializes index updates across processes using a session lock.
// User transactions only enqueue IDs; they never wait for Meilisearch.
type SearchSync struct {
	db     *pgxpool.Pool
	engine *search.Client
	logger *slog.Logger
}

func NewSearchSync(db *pgxpool.Pool, engine *search.Client, logger *slog.Logger) *SearchSync {
	return &SearchSync{db: db, engine: engine, logger: logger}
}
func (s *SearchSync) Run(ctx context.Context) {
	failed := false
	for {
		pass, cancel := context.WithTimeout(ctx, 90*time.Second)
		err := s.SyncOnce(pass)
		cancel()
		delay := time.Second
		if err != nil {
			s.engine.SetReady(false)
			if ctx.Err() != nil {
				return
			}
			if !failed {
				s.logger.Warn("Meilisearch unavailable; using PostgreSQL search", "error", err)
			}
			failed = true
			delay = 5 * time.Second
		} else if s.engine.Ready() && failed {
			s.logger.Info("Meilisearch search restored")
			failed = false
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *SearchSync) SyncOnce(ctx context.Context) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(73819205)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var released bool
		if e := conn.QueryRow(unlock, `SELECT pg_advisory_unlock(73819205)`).Scan(&released); e != nil || !released {
			_ = conn.Conn().Close(context.Background())
		}
	}()
	if !s.engine.Ready() {
		if err = s.engine.Reset(ctx); err != nil {
			return err
		}
		var cursor int64
		for {
			rows, e := conn.Query(ctx, `SELECT l.id,l.revision,l.title,l.description`+source+` WHERE `+visible+` AND l.id>$1 ORDER BY l.id LIMIT 100`, cursor)
			if e != nil {
				return e
			}
			docs := []search.Document{}
			for rows.Next() {
				var d search.Document
				if e = rows.Scan(&d.ID, &d.Revision, &d.Title, &d.Description); e != nil {
					rows.Close()
					return e
				}
				docs = append(docs, d)
				cursor = d.ID
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if len(docs) == 0 {
				break
			}
			if err = s.engine.Put(ctx, docs); err != nil {
				return err
			}
		}
	}
	for batch := 0; batch < 20; batch++ {
		rows, e := conn.Query(ctx, `SELECT q.listing_id,q.version,l.revision,l.title,l.description
   FROM search_index_queue q
   LEFT JOIN (listings l JOIN users u ON u.id=l.author_id) ON l.id=q.listing_id AND (`+visible+`)
   ORDER BY q.version LIMIT 100`)
		if e != nil {
			return e
		}
		type job struct{ id, version int64 }
		jobs := []job{}
		docs := []search.Document{}
		deleted := []int64{}
		for rows.Next() {
			var j job
			var revision *int64
			var title, description *string
			if e = rows.Scan(&j.id, &j.version, &revision, &title, &description); e != nil {
				rows.Close()
				return e
			}
			jobs = append(jobs, j)
			if revision == nil {
				deleted = append(deleted, j.id)
			} else {
				docs = append(docs, search.Document{ID: j.id, Revision: *revision, Title: *title, Description: *description})
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(jobs) == 0 {
			break
		}
		// Wait for both asynchronous operations before acknowledging this snapshot.
		if err = s.engine.Put(ctx, docs); err != nil {
			return err
		}
		if err = s.engine.Delete(ctx, deleted); err != nil {
			return err
		}
		ids, versions := []int64{}, []int64{}
		for _, j := range jobs {
			ids = append(ids, j.id)
			versions = append(versions, j.version)
		}
		// Skip rows held by a user transaction. A batched acknowledgement must
		// not deadlock with account deletion or an author-wide moderation change.
		if _, err = conn.Exec(ctx, `WITH confirmed AS MATERIALIZED (
 SELECT q.listing_id FROM search_index_queue q
 JOIN unnest($1::bigint[],$2::bigint[]) sent(id,version)
 ON q.listing_id=sent.id AND q.version=sent.version
 FOR UPDATE OF q SKIP LOCKED
 ) DELETE FROM search_index_queue q USING confirmed c WHERE q.listing_id=c.listing_id`, ids, versions); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !s.engine.Ready() {
		s.logger.Info("Meilisearch index ready")
	}
	s.engine.SetReady(true)
	return nil
}
