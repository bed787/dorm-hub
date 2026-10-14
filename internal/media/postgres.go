package media

import (
	"context"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/platform/idempotency"
	"dorm-hub/internal/platform/postgres"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

func lock(ctx context.Context, tx pgx.Tx, listing, owner int64) error {
	var active, blocked bool
	if err := tx.QueryRow(ctx, `SELECT active,blocked FROM users WHERE id=$1 FOR UPDATE`, owner).Scan(&active, &blocked); err != nil {
		return postgres.Error(err)
	}
	if !active || blocked {
		return domain.ErrForbidden
	}
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM listings WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL FOR UPDATE`, listing, owner).Scan(&id); err != nil {
		return postgres.Error(err)
	}
	return nil
}
func (s *Service) Upload(ctx context.Context, listing, owner int64, data []byte) (listings.Photo, error) {
	return s.UploadOnce(ctx, listing, owner, data, "")
}

func (s *Service) UploadOnce(ctx context.Context, listing, owner int64, data []byte, key string) (listings.Photo, error) {
	return s.UploadFrom(ctx, listing, owner, key, func() ([]byte, error) { return data, nil })
}

// Admission happens before reading the request body. At most four uploads can
// hold their file buffers, including the upload currently being decoded.
func (s *Service) UploadFrom(ctx context.Context, listing, owner int64, key string, read func() ([]byte, error)) (listings.Photo, error) {
	if err := ctx.Err(); err != nil {
		return listings.Photo{}, err
	}
	if key != "" {
		if err := idempotency.Validate(key); err != nil {
			return listings.Photo{}, err
		}
	}
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	default:
		return listings.Photo{}, &domain.Error{Kind: domain.ErrRateLimit, Message: "Сейчас загружается слишком много фотографий. Повторите позже"}
	}
	data, err := read()
	if err != nil {
		return listings.Photo{}, err
	}
	return s.upload(ctx, listing, owner, data, key)
}

func (s *Service) upload(ctx context.Context, listing, owner int64, data []byte, key string) (listings.Photo, error) {
	if len(data) == 0 || len(data) > MaxFileSize {
		return listings.Photo{}, domain.Invalid("Размер фотографии должен быть не больше 5 МБ")
	}
	var allowed bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM listings l JOIN users u ON u.id=l.author_id WHERE l.id=$1 AND l.author_id=$2 AND l.deleted_at IS NULL AND u.active AND NOT u.blocked)`, listing, owner).Scan(&allowed); err != nil {
		return listings.Photo{}, err
	}
	if !allowed {
		return listings.Photo{}, domain.ErrNotFound
	}
	scope := "photo.upload:" + strconv.FormatInt(listing, 10)
	requestHash := idempotency.Hash(data)
	result, found, err := idempotency.Lookup(ctx, s.db, owner, scope, key, requestHash)
	if err != nil {
		return listings.Photo{}, err
	}
	if found {
		return savedPhoto(ctx, s.db, listing, result)
	}
	// Reject already full listings before spending CPU on decoding.
	var count int
	if err = s.db.QueryRow(ctx, `SELECT count(*) FROM photos WHERE listing_id=$1`, listing).Scan(&count); err != nil {
		return listings.Photo{}, err
	}
	if count >= 10 {
		return listings.Photo{}, domain.Conflict("Можно прикрепить не больше 10 фотографий")
	}
	if err = s.checkQuota(ctx, s.db, owner, 0); err != nil {
		return listings.Photo{}, err
	}
	normalized, width, height, err := s.normalize(ctx, data)
	if err != nil {
		return listings.Photo{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return listings.Photo{}, err
	}
	defer tx.Rollback(context.Background())
	if err = lock(ctx, tx, listing, owner); err != nil {
		return listings.Photo{}, err
	}
	result, found, err = idempotency.Lookup(ctx, tx, owner, scope, key, requestHash)
	if err != nil {
		return listings.Photo{}, err
	}
	if found {
		return savedPhoto(ctx, tx, listing, result)
	}
	var position int
	if err = tx.QueryRow(ctx, `SELECT count(*),coalesce(max(position)+1,0) FROM photos WHERE listing_id=$1`, listing).Scan(&count, &position); err != nil {
		return listings.Photo{}, err
	}
	if count >= 10 {
		return listings.Photo{}, domain.Conflict("Можно прикрепить не больше 10 фотографий")
	}
	// The owner lock serializes quota checks across all of their listings.
	if err = s.checkQuota(ctx, tx, owner, int64(len(normalized))); err != nil {
		return listings.Photo{}, err
	}
	id, err := auth.Token()
	if err != nil {
		return listings.Photo{}, err
	}
	name := id + ".jpg"
	if err = s.files.Save(name, normalized); err != nil {
		return listings.Photo{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.files.Delete(name)
		}
	}()
	_, err = tx.Exec(ctx, `INSERT INTO photos(id,listing_id,position,filename,width,height,size_bytes) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, listing, position, name, width, height, len(normalized))
	if err != nil {
		return listings.Photo{}, postgres.Error(err)
	}
	if err = idempotency.Record(ctx, tx, owner, scope, key, requestHash, id); err != nil {
		return listings.Photo{}, err
	}
	if err = touchListing(ctx, tx, listing); err != nil {
		return listings.Photo{}, err
	}
	// A lost COMMIT response may mean the transaction committed. Keep its file
	// in that case; deleting it would break the persisted retry result.
	committed = true
	if err = tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			committed = false
		}
		return listings.Photo{}, err
	}
	return listings.Photo{ID: id, Width: width, Height: height, Position: position}, nil
}

func savedPhoto(ctx context.Context, q idempotency.Querier, listing int64, id string) (listings.Photo, error) {
	var p listings.Photo
	err := q.QueryRow(ctx, `SELECT id,width,height,position FROM photos WHERE listing_id=$1 AND id=$2`, listing, id).Scan(&p.ID, &p.Width, &p.Height, &p.Position)
	return p, postgres.Error(err)
}

func (s *Service) checkQuota(ctx context.Context, q idempotency.Querier, owner, incoming int64) error {
	var count int
	var used int64
	if err := q.QueryRow(ctx, `SELECT count(*),coalesce(sum(p.size_bytes),0)::bigint FROM photos p JOIN listings l ON l.id=p.listing_id WHERE l.author_id=$1`, owner).Scan(&count, &used); err != nil {
		return err
	}
	if count >= s.photoLimit || used >= s.quotaBytes || incoming > s.quotaBytes-used {
		return domain.Conflict("Достигнута квота фотографий аккаунта. Удалите ненужные фото из своих объявлений")
	}
	return nil
}

func (s *Service) find(ctx context.Context, id string, viewer int64, admin bool) (string, time.Time, error) {
	var name string
	var created time.Time
	err := s.db.QueryRow(ctx, `SELECT p.filename,p.created_at FROM photos p JOIN listings l ON l.id=p.listing_id JOIN users u ON u.id=l.author_id WHERE p.id=$1 AND l.deleted_at IS NULL AND (l.author_id=$2 OR $3 OR (NOT l.hidden AND u.active AND NOT u.blocked AND l.status='active'))`, id, viewer, admin).Scan(&name, &created)
	return name, created, postgres.Error(err)
}
func (s *Service) Delete(ctx context.Context, id string, listing, owner int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lock(ctx, tx, listing, owner); err != nil {
		return err
	}
	var name string
	if err = tx.QueryRow(ctx, `DELETE FROM photos WHERE id=$1 AND listing_id=$2 RETURNING filename`, id, listing).Scan(&name); err != nil {
		return postgres.Error(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE photos p SET position=n.pos FROM (SELECT id,(row_number() OVER(ORDER BY position)-1)::integer pos FROM photos WHERE listing_id=$1) n WHERE p.id=n.id`, listing); err != nil {
		return err
	}
	if err = touchListing(ctx, tx, listing); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO pending_file_deletions(filename) VALUES($1) ON CONFLICT DO NOTHING`, name); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return s.FlushDeletions(ctx)
}
func (s *Service) Reorder(ctx context.Context, listing, owner int64, ids []string) error {
	if len(ids) > 10 {
		return domain.Invalid("Не больше 10 фотографий")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if len(id) != 64 || seen[id] {
			return domain.Invalid("Неверный список фотографий")
		}
		seen[id] = true
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lock(ctx, tx, listing, owner); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM photos WHERE listing_id=$1`, listing).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return domain.Conflict("Список фотографий изменился. Обновите страницу")
	}
	for position, id := range ids {
		tag, err := tx.Exec(ctx, `UPDATE photos SET position=$3 WHERE listing_id=$1 AND id=$2`, listing, id, position)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return domain.Invalid("Фотография не принадлежит объявлению")
		}
	}
	if err = touchListing(ctx, tx, listing); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Photo edits invalidate publication/review snapshots. Completed entries stay
// completed and private; drafts remain in their author's archive.
func touchListing(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `UPDATE listings SET status=CASE WHEN status='active' THEN 'pending' ELSE status END,revision=revision+1,updated_at=now() WHERE id=$1`, id)
	return err
}
func (s *Service) FlushDeletions(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT filename FROM pending_file_deletions ORDER BY created_at LIMIT 200`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err = s.files.Delete(name); err != nil {
			return err
		}
		if _, err = s.db.Exec(ctx, `DELETE FROM pending_file_deletions WHERE filename=$1`, name); err != nil {
			return err
		}
	}
	if len(names) == 200 {
		return s.FlushDeletions(ctx)
	}
	return nil
}
