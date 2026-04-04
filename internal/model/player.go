package model

import (
	"time"

	"cloud.google.com/go/civil"
)

// Player maps to the players table.
type Player struct {
	PlayerID         string     `json:"player_id" db:"player_id"`
	FirebaseUID      string     `json:"firebase_uid" db:"firebase_uid"`
	Username         string     `json:"username" db:"username"`
	Level            int64      `json:"level" db:"level"`
	Exp              int64      `json:"exp" db:"exp"`
	IsPremium        bool       `json:"is_premium" db:"is_premium"`
	EquippedIconNo   *int64     `json:"equipped_icon_no,omitempty" db:"equipped_icon_no"`
	SelectedFaction  *string    `json:"selected_faction,omitempty" db:"selected_faction"`
	PremiumExpiresAt *time.Time `json:"premium_expires_at,omitempty" db:"premium_expires_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

// PlayerDailyBattle maps to the player_daily_battles table.
type PlayerDailyBattle struct {
	PlayerID         string     `json:"player_id" db:"player_id"`
	DailyBattleCount int64      `json:"daily_battle_count" db:"daily_battle_count"`
	LastResetDate    civil.Date `json:"last_reset_date" db:"last_reset_date"`
}

// PlayerCard maps to the player_cards table.
type PlayerCard struct {
	PlayerID string `json:"player_id" db:"player_id"`
	CardID   string `json:"card_id" db:"card_id"`
	ArtNo    int64  `json:"art_no" db:"art_no"`
	Count    int    `json:"count" db:"count"`
}

// GameConfig maps to the game_config table.
type GameConfig struct {
	Key       string    `json:"key" db:"key"`
	Value     string    `json:"value" db:"value"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
