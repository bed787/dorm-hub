package listings

import (
	"context"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/platform/idempotency"
	"dorm-hub/internal/platform/postgres"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
	"strings"
	"unicode/utf8"
)

const effective = `l.status`
const visible = `l.deleted_at IS NULL AND NOT l.hidden AND u.active AND NOT u.blocked AND l.status='active'`
const selection = `l.id,l.author_id,u.nickname,l.kind,coalesce(l.category,''),l.title,l.description,coalesce(l.location,''),l.address,l.dorm_id,coalesce(d.name,''),l.price_mode,l.price_minor,l.telegram,l.vk,` + effective + `,l.completed_at,l.hidden,l.created_at,l.updated_at,l.archive_kind,l.moderation_comment,l.revision,l.completion_reason,
 EXISTS(SELECT 1 FROM favorites f WHERE f.listing_id=l.id AND f.user_id=$1),
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',p.id,'width',p.width,'height',p.height,'position',p.position) ORDER BY p.position) FROM photos p WHERE p.listing_id=l.id),'[]'::jsonb)`
const source = ` FROM listings l JOIN users u ON u.id=l.author_id LEFT JOIN dorms d ON d.id=l.dorm_id `

func scan(row pgx.Row) (Listing, error) {
	var l Listing
	var photos []byte
	err := row.Scan(&l.ID, &l.AuthorID, &l.AuthorName, &l.Kind, &l.Category, &l.Title, &l.Description, &l.Location, &l.Address, &l.DormID, &l.DormName, &l.PriceMode, &l.PriceMinor, &l.Telegram, &l.VK, &l.Status, &l.CompletedAt, &l.Hidden, &l.CreatedAt, &l.UpdatedAt, &l.ArchiveKind, &l.ModerationComment, &l.Revision, &l.CompletionReason, &l.Favorite, &photos)
	if err != nil {
		return l, postgres.Error(err)
	}
	if err = json.Unmarshal(photos, &l.Photos); err != nil {
		return l, err
	}
	return l, nil
}
func (s *Service) Get(ctx context.Context, id, viewer int64, admin bool) (Listing, error) {
	return scan(s.db.QueryRow(ctx, `SELECT `+selection+source+` WHERE l.id=$2 AND l.deleted_at IS NULL AND ((`+visible+`) OR l.author_id=$1 OR $3)`, viewer, id, admin))
}
func (s *Service) List(ctx context.Context, viewer int64, f Filter) (Page, error) {
	return s.ListForViewer(ctx, viewer, f, false)
}
func (s *Service) ListForViewer(ctx context.Context, viewer int64, f Filter, admin bool) (Page, error) {
	page := Page{Items: []Listing{}, Limit: f.Limit, Offset: f.Offset}
	if f.Limit < 1 || f.Limit > 50 {
		f.Limit = 24
		page.Limit = 24
	}
	if f.Offset < 0 {
		return page, domain.Invalid("Неверная страница")
	}
	if utf8.RuneCountInString(f.Query) > 200 {
		return page, domain.Invalid("Поисковый запрос слишком длинный")
	}
	args := []any{viewer}
	conditions := []string{}
	add := func(expr string, v any) {
		args = append(args, v)
		conditions = append(conditions, fmt.Sprintf(expr, len(args)))
	}
	switch f.Scope {
	case "mine":
		conditions = append(conditions, `l.author_id=$1 AND l.deleted_at IS NULL`)
	case "review":
		if !admin {
			return page, domain.ErrForbidden
		}
		conditions = append(conditions, `l.status='pending' AND l.deleted_at IS NULL AND u.active AND NOT u.blocked`)
	case "admin_author":
		if !admin {
			return page, domain.ErrForbidden
		}
		if f.AuthorID <= 0 {
			return page, domain.Invalid("Укажите автора для просмотра модератором")
		}
		conditions = append(conditions, `l.deleted_at IS NULL`)
	case "favorites":
		conditions = append(conditions, visible, `EXISTS(SELECT 1 FROM favorites f WHERE f.listing_id=l.id AND f.user_id=$1)`)
	case "", "feed":
		conditions = append(conditions, visible)
	default:
		return page, domain.Invalid("Неверный список объявлений")
	}
	if f.Status != "" {
		if (f.Scope != "mine" && f.Scope != "admin_author") || !oneOf(f.Status, "active", "pending", "completed", "archived") {
			return page, domain.Invalid("Неверный статус")
		}
		add(effective+`=$%d`, f.Status)
	}
	if f.Kind != "" {
		if !oneOf(f.Kind, "offer", "request") {
			return page, domain.Invalid("Неверный тип")
		}
		add(`l.kind=$%d`, f.Kind)
	}
	if f.Category != "" {
		if !oneOf(f.Category, "things", "study", "help", "housing", "other") {
			return page, domain.Invalid("Неверная категория")
		}
		add(`l.category=$%d`, f.Category)
	}
	if f.Location != "" {
		if !oneOf(f.Location, "VASILEOSTROVSKY", "PETRODVORTSOVY", "NEVSKY", "EXTERNAL") {
			return page, domain.Invalid("Неверное местоположение")
		}
		add(`l.location=$%d`, f.Location)
	}
	if f.DormID > 0 {
		add(`l.dorm_id=$%d`, f.DormID)
	}
	if f.AuthorID > 0 {
		add(`l.author_id=$%d`, f.AuthorID)
	}
	order := `l.created_at DESC,l.id DESC`
	if strings.TrimSpace(f.Query) != "" {
		add(`l.search_vector @@ websearch_to_tsquery('russian',$%d)`, f.Query)
		order = fmt.Sprintf(`ts_rank_cd(l.search_vector,websearch_to_tsquery('russian',$%d)) DESC,l.created_at DESC,l.id DESC`, len(args))
	}
	where := ` WHERE ` + strings.Join(conditions, ` AND `)
	// The viewer parameter is retained even when the count query only uses other filters.
	if err := s.db.QueryRow(ctx, `SELECT count(*)`+source+where+` AND $1::bigint>0`, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	args = append(args, f.Limit, f.Offset)
	query := `SELECT ` + selection + source + where + ` ORDER BY ` + order + fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scan(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, l)
	}
	return page, rows.Err()
}
func lockAuthor(ctx context.Context, tx pgx.Tx, author int64) error {
	var active, blocked bool
	if err := tx.QueryRow(ctx, `SELECT active,blocked FROM users WHERE id=$1 FOR UPDATE`, author).Scan(&active, &blocked); err != nil {
		return postgres.Error(err)
	}
	if !active || blocked {
		return domain.ErrForbidden
	}
	return nil
}
func checkLimit(ctx context.Context, tx pgx.Tx, author int64) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM listings WHERE author_id=$1 AND status IN ('active','pending') AND deleted_at IS NULL`, author).Scan(&count); err != nil {
		return err
	}
	if count >= 10 {
		return domain.Conflict("Можно иметь не более 10 опубликованных объявлений и объявлений на проверке")
	}
	return nil
}

// Call while holding the author lock, for every user path entering the archive.
func checkArchiveLimit(ctx context.Context, tx pgx.Tx, author int64) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM listings WHERE author_id=$1 AND status='archived' AND deleted_at IS NULL`, author).Scan(&count); err != nil {
		return err
	}
	if count >= 50 {
		return domain.Conflict("В архиве можно хранить до 50 объявлений. Удалите ненужные")
	}
	return nil
}
func checkDorm(ctx context.Context, tx pgx.Tx, in Input) error {
	if in.DormID == nil {
		return nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dorms WHERE id=$1 AND location=$2)`, *in.DormID, in.Location).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.Invalid("Общежитие не относится к выбранному местоположению")
	}
	return nil
}
func (s *Service) Create(ctx context.Context, author int64, in Input) (Listing, error) {
	return s.CreateOnce(ctx, author, in, "")
}
func (s *Service) CreateOnce(ctx context.Context, author int64, in Input, key string) (Listing, error) {
	if key != "" {
		if err := idempotency.Validate(key); err != nil {
			return Listing{}, err
		}
	}
	in = clean(in)
	validate := Validate
	if in.Draft {
		validate = ValidateDraft
	}
	if err := validate(in); err != nil {
		return Listing{}, err
	}
	if in.Draft && !HasContent(in) {
		return Listing{}, domain.Invalid("Пустой черновик не сохраняется")
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		return Listing{}, err
	}
	hash := idempotency.Hash(encoded)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Listing{}, err
	}
	defer tx.Rollback(context.Background())
	if err = lockAuthor(ctx, tx, author); err != nil {
		return Listing{}, err
	}
	result, found, err := idempotency.Lookup(ctx, tx, author, "listing.create", key, hash)
	if err != nil {
		return Listing{}, err
	}
	if found {
		id, err := strconv.ParseInt(result, 10, 64)
		if err != nil {
			return Listing{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return Listing{}, err
		}
		return s.Get(ctx, id, author, false)
	}
	status, archive := "pending", ""
	if in.Draft {
		status, archive = "archived", "draft"
		if err = checkArchiveLimit(ctx, tx, author); err != nil {
			return Listing{}, err
		}
	} else if err = checkLimit(ctx, tx, author); err != nil {
		return Listing{}, err
	}
	if err = checkDorm(ctx, tx, in); err != nil {
		return Listing{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO listings(author_id,kind,category,title,description,location,dorm_id,price_mode,price_minor,telegram,vk,status,archive_kind,address) VALUES($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, author, in.Kind, in.Category, in.Title, in.Description, in.Location, in.DormID, in.PriceMode, in.PriceMinor, in.Telegram, in.VK, status, archive, in.Address).Scan(&id)
	if err != nil {
		return Listing{}, postgres.Error(err)
	}
	if err = idempotency.Record(ctx, tx, author, "listing.create", key, hash, strconv.FormatInt(id, 10)); err != nil {
		return Listing{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Listing{}, err
	}
	return s.Get(ctx, id, author, false)
}
func (s *Service) Update(ctx context.Context, id, author int64, in Input) (Listing, error) {
	in = clean(in)
	validate := Validate
	if in.Draft {
		validate = ValidateDraft
	}
	if err := validate(in); err != nil {
		return Listing{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Listing{}, err
	}
	defer tx.Rollback(context.Background())
	if err = lockAuthor(ctx, tx, author); err != nil {
		return Listing{}, err
	}
	var status, archive string
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT status,archive_kind,revision FROM listings WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, author).Scan(&status, &archive, &revision); err != nil {
		return Listing{}, postgres.Error(err)
	}
	if in.Revision > 0 && in.Revision != revision {
		return Listing{}, domain.Conflict("Объявление изменилось в другой вкладке. Обновите страницу")
	}
	if err = checkDorm(ctx, tx, in); err != nil {
		return Listing{}, err
	}
	// Editing completed entries never republishes them. A previously published
	// entry is hidden as soon as any draft update is committed.
	if status != "completed" {
		if in.Draft {
			if status != "archived" {
				if err = checkArchiveLimit(ctx, tx, author); err != nil {
					return Listing{}, err
				}
			}
			status = "archived"
			if archive == "" {
				archive = "draft"
			}
		} else {
			if status != "active" && status != "pending" {
				if err = checkLimit(ctx, tx, author); err != nil {
					return Listing{}, err
				}
			}
			status, archive = "pending", ""
		}
	}
	_, err = tx.Exec(ctx, `UPDATE listings SET kind=$3,category=NULLIF($4,''),title=$5,description=$6,location=NULLIF($7,''),dorm_id=$8,price_mode=$9,price_minor=$10,telegram=$11,vk=$12,status=$13,archive_kind=$14,address=$15,revision=revision+1,updated_at=now() WHERE id=$1 AND author_id=$2`, id, author, in.Kind, in.Category, in.Title, in.Description, in.Location, in.DormID, in.PriceMode, in.PriceMinor, in.Telegram, in.VK, status, archive, in.Address)
	if err != nil {
		return Listing{}, postgres.Error(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Listing{}, err
	}
	return s.Get(ctx, id, author, false)
}
func (s *Service) Transition(ctx context.Context, id, author int64, action string) (Listing, error) {
	return s.TransitionWithOutcome(ctx, id, author, action, "success")
}
func (s *Service) TransitionWithOutcome(ctx context.Context, id, author int64, action, outcome string) (Listing, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Listing{}, err
	}
	defer tx.Rollback(context.Background())
	if err = lockAuthor(ctx, tx, author); err != nil {
		return Listing{}, err
	}
	var status string
	var hidden, counted bool
	if err = tx.QueryRow(ctx, `SELECT status,hidden,completion_counted FROM listings WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, author).Scan(&status, &hidden, &counted); err != nil {
		return Listing{}, postgres.Error(err)
	}
	switch action {
	case "complete":
		if status != "active" {
			return Listing{}, domain.Conflict("Завершить можно только опубликованное объявление")
		}
		if outcome != "success" && outcome != "no_longer_needed" {
			return Listing{}, domain.Invalid("Укажите причину завершения")
		}
		if outcome == "success" && !counted {
			if _, err = tx.Exec(ctx, `UPDATE site_statistics SET completed_deals=completed_deals+1 WHERE singleton`); err != nil {
				return Listing{}, err
			}
			counted = true
		}
		_, err = tx.Exec(ctx, `UPDATE listings SET status='completed',completed_at=now(),completion_reason=$2,completion_counted=$3,archive_kind='',revision=revision+1,updated_at=now() WHERE id=$1`, id, outcome, counted)
	case "archive":
		if status != "archived" {
			if err = checkArchiveLimit(ctx, tx, author); err != nil {
				return Listing{}, err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE listings SET status='archived',archive_kind='held',revision=revision+1,updated_at=now() WHERE id=$1`, id)
	case "submit", "activate":
		if hidden {
			return Listing{}, domain.Forbidden("Объявление скрыто модератором")
		}
		if status == "pending" {
			if err = tx.Commit(ctx); err != nil {
				return Listing{}, err
			}
			return s.Get(ctx, id, author, false)
		}
		if status == "active" {
			return Listing{}, domain.Conflict("Объявление уже опубликовано")
		}
		l, err := scan(tx.QueryRow(ctx, `SELECT `+selection+source+` WHERE l.id=$2`, author, id))
		if err != nil {
			return Listing{}, err
		}
		in := Input{Kind: l.Kind, Category: l.Category, Title: l.Title, Description: l.Description, Location: l.Location, Address: l.Address, DormID: l.DormID, PriceMode: l.PriceMode, PriceMinor: l.PriceMinor, Telegram: l.Telegram, VK: l.VK}
		if err = Validate(in); err != nil {
			return Listing{}, err
		}
		if err = checkDorm(ctx, tx, in); err != nil {
			return Listing{}, err
		}
		if err = checkLimit(ctx, tx, author); err != nil {
			return Listing{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE listings SET status='pending',archive_kind='',completed_at=NULL,completion_reason='',revision=revision+1,updated_at=now() WHERE id=$1`, id)
	default:
		return Listing{}, domain.Invalid("Неверное действие")
	}
	if err != nil {
		return Listing{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Listing{}, err
	}
	return s.Get(ctx, id, author, false)
}
func (s *Service) Review(ctx context.Context, id, actor, revision int64, approve bool, comment string) (Listing, error) {
	comment = strings.TrimSpace(comment)
	if !approve && (utf8.RuneCountInString(comment) < 3 || utf8.RuneCountInString(comment) > 2000 || strings.ContainsRune(comment, 0)) {
		return Listing{}, domain.Invalid("Укажите причину отказа: от 3 до 2000 символов")
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
	if err = lockAuthor(ctx, tx, owner); err != nil {
		return Listing{}, err
	}
	var status string
	var current int64
	var hidden bool
	if err = tx.QueryRow(ctx, `SELECT status,revision,hidden FROM listings WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&status, &current, &hidden); err != nil {
		return Listing{}, postgres.Error(err)
	}
	if status != "pending" || revision < 1 || current != revision {
		return Listing{}, domain.Conflict("Объявление уже изменено или проверено. Обновите очередь")
	}
	if approve && hidden {
		return Listing{}, domain.Conflict("Сначала снимите блокировку объявления")
	}
	status, archive := "archived", "rejected"
	action := "reject"
	if approve {
		status, archive, comment, action = "active", "", "", "approve"
	}
	_, err = tx.Exec(ctx, `UPDATE listings SET status=$2,archive_kind=$3,moderation_comment=$4,revision=revision+1,created_at=CASE WHEN $2='active' THEN now() ELSE created_at END,updated_at=now() WHERE id=$1`, id, status, archive, comment)
	if err != nil {
		return Listing{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO moderation_actions(actor_id,target_type,target_id,action,reason) VALUES($1,'listing',$2,$3,$4)`, actor, id, action, comment); err != nil {
		return Listing{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Listing{}, err
	}
	return s.Get(ctx, id, actor, true)
}
func (s *Service) Delete(ctx context.Context, id, author int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = lockAuthor(ctx, tx, author); err != nil {
		return err
	}
	var exists int64
	if err = tx.QueryRow(ctx, `SELECT id FROM listings WHERE id=$1 AND author_id=$2 FOR UPDATE`, id, author).Scan(&exists); err != nil {
		return postgres.Error(err)
	}
	for _, query := range []string{
		`INSERT INTO pending_file_deletions(filename) SELECT filename FROM photos WHERE listing_id=$1 ON CONFLICT DO NOTHING`,
		`DELETE FROM reports WHERE listing_id=$1 OR (target_type='listing' AND target_id=$1)`,
		`DELETE FROM moderation_actions WHERE target_type='listing' AND target_id=$1`,
		`DELETE FROM listings WHERE id=$1`,
	} {
		if _, err = tx.Exec(ctx, query, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (s *Service) CompletedDeals(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.QueryRow(ctx, `SELECT completed_deals FROM site_statistics WHERE singleton`).Scan(&total)
	return total, err
}
