package listings

import (
	"context"
	"crypto/sha256"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/locations"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Section struct {
	Code  string    `json:"code"`
	Name  string    `json:"name"`
	Total int       `json:"total"`
	Items []Listing `json:"items"`
}
type Showcase struct {
	Sections   []Section      `json:"sections"`
	Total      int            `json:"total"`
	KindTotals map[string]int `json:"kind_totals"`
	Seed       string         `json:"seed"`
}

// Only the overview is mixed. Search and complete category pages keep their
// existing relevance/date order. A supplied seed preserves a browsing session.
func (s *Service) Showcase(ctx context.Context, viewer int64, f Filter, seed string) (Showcase, error) {
	result := Showcase{Sections: []Section{}, KindTotals: map[string]int{"offer": 0, "request": 0}}
	if f.Scope != "" && f.Scope != "feed" || f.Query != "" || f.Category != "" || f.Status != "" || f.AuthorID != 0 {
		return result, domain.Invalid("Неверные параметры ленты")
	}
	if f.Kind == "" {
		f.Kind = "offer"
	}
	if !oneOf(f.Kind, "offer", "request") || f.Location != "" && !locations.ValidLocation(f.Location) || f.DormID < 0 {
		return result, domain.Invalid("Неверный фильтр")
	}
	if seed == "" {
		seed = strconv.FormatInt(time.Now().Unix()/14400, 10)
	}
	if len(seed) > 13 {
		return result, domain.Invalid("Неверный порядок ленты")
	}
	if value, err := strconv.ParseInt(seed, 10, 64); err != nil || value < 0 {
		return result, domain.Invalid("Неверный порядок ленты")
	}
	result.Seed = seed
	args := []any{viewer}
	where := visible
	if f.Location != "" {
		args = append(args, f.Location)
		where += fmt.Sprintf(" AND l.location=$%d", len(args))
	}
	if f.DormID > 0 {
		args = append(args, f.DormID)
		where += fmt.Sprintf(" AND l.dorm_id=$%d", len(args))
	}
	// Count every matching row, not just the bounded previews.
	rows, err := s.db.Query(ctx, `SELECT l.kind,l.category,c.name,count(*) FROM listings l JOIN users u ON u.id=l.author_id JOIN categories c ON c.code=l.category WHERE `+where+` AND $1::bigint>0 GROUP BY l.kind,l.category,c.name`, args...)
	if err != nil {
		return result, err
	}
	sections := map[string]*Section{}
	for rows.Next() {
		var kind, code, name string
		var count int
		if err = rows.Scan(&kind, &code, &name, &count); err != nil {
			rows.Close()
			return result, err
		}
		result.KindTotals[kind] += count
		if kind == f.Kind {
			sections[code] = &Section{Code: code, Name: name, Total: count, Items: []Listing{}}
			result.Total += count
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	args = append(args, f.Kind)
	where += fmt.Sprintf(" AND l.kind=$%d", len(args))
	args = append(args, seed)
	seedParam := fmt.Sprintf("$%d", len(args))
	// Two bounded candidate sets per category: newest and rotating, with one
	// candidate per author before selecting their second. No unbounded text scan
	// into application memory and no per-user viewing history.
	query := `WITH candidates AS (
 SELECT l.id,l.category,l.created_at,
 row_number() OVER(PARTITION BY l.category ORDER BY l.created_at DESC,l.id DESC) AS fresh_rank,
 row_number() OVER(PARTITION BY l.category,l.author_id ORDER BY md5(l.id::text||` + seedParam + `),l.id) AS author_rank,
 md5(l.id::text||` + seedParam + `) AS shuffle
 FROM listings l JOIN users u ON u.id=l.author_id WHERE ` + where + `
 ), rotated AS (
 SELECT *,row_number() OVER(PARTITION BY category ORDER BY author_rank,shuffle,id) AS rotating_rank FROM candidates
 ), picks AS (SELECT id FROM rotated WHERE fresh_rank<=16 OR rotating_rank<=32)
 SELECT ` + selection + source + ` JOIN picks ON picks.id=l.id WHERE ` + where + ` ORDER BY l.created_at DESC,l.id DESC`
	rows, err = s.db.Query(ctx, query, args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		l, e := scan(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		if section := sections[l.Category]; section != nil {
			section.Items = append(section.Items, l)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	order := []string{"things", "clothes", "accessories", "home", "study", "help", "other"}
	for _, code := range order {
		if section := sections[code]; section != nil {
			section.Items = mixListings(section.Items, seed, 8)
			for i := range section.Items {
				l := &section.Items[i]
				l.Description = ""
				l.Telegram = ""
				l.VK = ""
				if len(l.Photos) > 1 {
					l.Photos = l.Photos[:1]
				}
			}
			if len(section.Items) > 0 {
				result.Sections = append(result.Sections, *section)
			}
		}
	}
	sort.SliceStable(result.Sections, func(i, j int) bool {
		a, b := result.Sections[i], result.Sections[j]
		if a.Code == "other" || b.Code == "other" {
			return a.Code != "other"
		}
		return a.Total > b.Total
	})
	return result, nil
}

func mixListings(items []Listing, seed string, limit int) []Listing {
	if len(items) == 0 || limit <= 0 {
		return []Listing{}
	}
	items = append([]Listing(nil), items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	n := min((limit+1)/2, (len(items)+1)/2)
	fresh, rotated := items[:n:n], append([]Listing(nil), items[n:]...)
	hash := func(l Listing) [32]byte { return sha256.Sum256([]byte(seed + ":" + strconv.FormatInt(l.ID, 10))) }
	sort.Slice(rotated, func(i, j int) bool {
		a, b := hash(rotated[i]), hash(rotated[j])
		return strings.Compare(string(a[:]), string(b[:])) < 0
	})
	result := make([]Listing, 0, min(limit, len(items)))
	seen := map[int64]int{}
	for len(result) < limit && len(fresh)+len(rotated) > 0 {
		preferred, other := &fresh, &rotated
		if len(result)%2 == 1 {
			preferred, other = &rotated, &fresh
		}
		if len(*preferred) == 0 {
			preferred, other = other, preferred
		}
		best := 0
		for i, l := range *preferred {
			if seen[l.AuthorID] < seen[(*preferred)[best].AuthorID] {
				best = i
			}
		}
		if seen[(*preferred)[best].AuthorID] > 0 {
			for i, l := range *other {
				if seen[l.AuthorID] == 0 {
					preferred, other = other, preferred
					best = i
					break
				}
			}
		}
		picked := (*preferred)[best]
		result = append(result, picked)
		seen[picked.AuthorID]++
		*preferred = append((*preferred)[:best], (*preferred)[best+1:]...)
	}
	return result
}
