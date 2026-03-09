package model

import (
	"encoding/json"
	"time"
)

// Product types
const (
	ProductTypeFactionSet   = "faction_set"
	ProductTypeCosmetic     = "cosmetic"
	ProductTypeSubscription = "subscription"
)

// Platform constants
const (
	PlatformIOS     = "ios"
	PlatformAndroid = "android"
)

// Cosmetic item types. DB seed (stamps.sql 等) と一致する必要がある。
const (
	ItemTypeStamp   = "stamp"
	ItemTypePlaymat = "playmat"
	ItemTypeSleeve  = "sleeve"
	ItemTypeIcon    = "icon"
)

// Subscription status constants
const (
	SubscriptionStatusActive    = "active"
	SubscriptionStatusExpired   = "expired"
	SubscriptionStatusCancelled = "cancelled"
	SubscriptionStatusGrace     = "grace_period"
	SubscriptionStatusRevoked   = "revoked"
)

// Product represents a purchasable item in the shop.
type Product struct {
	ProductID string          `json:"product_id" db:"product_id"`
	Name      string          `json:"name" db:"name"`
	Type      string          `json:"type" db:"type"`
	Price     int64           `json:"price" db:"price"`
	Content   json.RawMessage `json:"content" db:"content"`
	IsActive  bool            `json:"is_active" db:"is_active"`
}

// FactionSetContent is the parsed content for faction_set products.
type FactionSetContent struct {
	Faction string `json:"faction"`
}

// CosmeticContent is the parsed content for cosmetic products.
type CosmeticContent struct {
	ItemType string `json:"item_type"`
	ItemNo   int64  `json:"item_no"`
}

// OneTimePurchase records a completed one-time purchase.
type OneTimePurchase struct {
	PlayerID      string    `json:"player_id" db:"player_id"`
	PurchaseID    int64     `json:"purchase_id" db:"purchase_id"`
	ProductID     string    `json:"product_id" db:"product_id"`
	Platform      string    `json:"platform" db:"platform"`
	PurchaseToken string    `json:"purchase_token" db:"purchase_token"`
	PurchasedAt   time.Time `json:"purchased_at" db:"purchased_at"`
}

// Subscription represents a player's subscription record.
type Subscription struct {
	PlayerID           string    `json:"player_id" db:"player_id"`
	SubscriptionID     int64     `json:"subscription_id" db:"subscription_id"`
	ProductID          string    `json:"product_id" db:"product_id"`
	Platform           string    `json:"platform" db:"platform"`
	PurchaseToken      string    `json:"purchase_token" db:"purchase_token"`
	Status             string    `json:"status" db:"status"`
	CurrentPeriodStart time.Time `json:"current_period_start" db:"current_period_start"`
	CurrentPeriodEnd   time.Time `json:"current_period_end" db:"current_period_end"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

// CosmeticItem represents a cosmetic item definition.
type CosmeticItem struct {
	ItemType      string  `json:"item_type" db:"item_type"`
	ItemNo        int64   `json:"item_no" db:"item_no"`
	ItemName      string  `json:"item_name" db:"item_name"`
	Description   *string `json:"description,omitempty" db:"description"`
	IsPurchasable bool    `json:"is_purchasable" db:"is_purchasable"`
	IsActive      bool    `json:"is_active" db:"is_active"`
}

// PlayerItem represents a cosmetic item owned by a player.
type PlayerItem struct {
	PlayerID   string    `json:"player_id" db:"player_id"`
	ItemType   string    `json:"item_type" db:"item_type"`
	ItemNo     int64     `json:"item_no" db:"item_no"`
	AcquiredAt time.Time `json:"acquired_at" db:"acquired_at"`
}
