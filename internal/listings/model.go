package listings

import "time"

type Photo struct {
	ID       string `json:"id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Position int    `json:"position"`
}
type Listing struct {
	ID                  int64      `json:"id"`
	AuthorID            int64      `json:"author_id"`
	AuthorName          string     `json:"author_name"`
	Kind                string     `json:"kind"`
	Category            string     `json:"category"`
	CategoryNeedsReview bool       `json:"category_needs_review,omitempty"`
	Title               string     `json:"title"`
	Description         string     `json:"description"`
	Location            string     `json:"location"`
	Address             string     `json:"address"`
	DormID              *int64     `json:"dorm_id"`
	DormName            string     `json:"dorm_name"`
	PriceMode           string     `json:"price_mode"`
	PriceMinor          *int64     `json:"price_minor"`
	Telegram            string     `json:"telegram"`
	VK                  string     `json:"vk"`
	ArchiveKind         string     `json:"archive_kind"`
	ModerationComment   string     `json:"moderation_comment"`
	Revision            int64      `json:"revision"`
	CompletionReason    string     `json:"completion_reason"`
	Status              string     `json:"status"`
	CompletedAt         *time.Time `json:"completed_at"`
	Hidden              bool       `json:"hidden"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	Favorite            bool       `json:"favorite"`
	Photos              []Photo    `json:"photos"`
}
type Input struct {
	HasPhotos   bool   `json:"has_photos,omitempty"`
	Draft       bool   `json:"draft"`
	Revision    int64  `json:"revision,omitempty"`
	Kind        string `json:"kind"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Address     string `json:"address"`
	DormID      *int64 `json:"dorm_id"`
	PriceMode   string `json:"price_mode"`
	PriceMinor  *int64 `json:"price_minor"`
	Telegram    string `json:"telegram"`
	VK          string `json:"vk"`
}
type Filter struct {
	Query, Kind, Category, Location, Scope, Status string
	DormID, AuthorID                               int64
	Limit, Offset                                  int
}
type Page struct {
	Items  []Listing `json:"items"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}
