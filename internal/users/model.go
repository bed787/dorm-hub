package users

import "time"

type User struct {
	ID              int64     `json:"id"`
	Email           string    `json:"-"`
	Nickname        string    `json:"nickname"`
	NicknameChanged bool      `json:"nickname_changed"`
	Telegram        string    `json:"telegram"`
	VK              string    `json:"vk"`
	Active          bool      `json:"active"`
	Blocked         bool      `json:"blocked"`
	IsAdmin         bool      `json:"is_admin"`
	CreatedAt       time.Time `json:"created_at"`
	Stars           int       `json:"stars"`
	Starred         bool      `json:"starred"`
}
type Update struct {
	Nickname string `json:"nickname"`
	Telegram string `json:"telegram"`
	VK       string `json:"vk"`
}
