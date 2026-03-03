package model

import "time"

// UserSettings holds per-player application settings.
type UserSettings struct {
	PlayerID    string    `json:"player_id"`
	Language    string    `json:"language"`
	BgmVolume   int64     `json:"bgm_volume"`
	SeVolume    int64     `json:"se_volume"`
	PushEnabled bool      `json:"push_enabled"`
	UpdatedAt   time.Time `json:"updated_at"`
}
