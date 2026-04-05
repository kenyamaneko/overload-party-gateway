// Re-export generated types from the shared package.
// This allows all existing code to continue importing "internal/model" unchanged.
package model

import (
	genmodel "github.com/kenyamaneko/overload-party-common/packages/gamedata/model"

	apimodel "github.com/kenyamaneko/overload-party-common/packages/api/model"
)

// ── API contract types (remain in common) ──

// Type aliases for card / deck types.
type CardDefinition = genmodel.CardDefinition
type ComputeStats = genmodel.ComputeStats
type DataStats = genmodel.DataStats
type PlayerCardWithDef = genmodel.PlayerCardWithDef
type Deck = genmodel.Deck
type DeckCard = genmodel.DeckCard

// Type aliases for effect types.
type PassiveEffect = genmodel.PassiveEffect
type PlatformEffect = genmodel.PlatformEffect
type AttachmentEffect = genmodel.AttachmentEffect
type PassiveEffectConfig = genmodel.PassiveEffectConfig
type PlatformEffectConfig = genmodel.PlatformEffectConfig
type AttachmentEffectConfig = genmodel.AttachmentEffectConfig

// Type aliases for effect named types.
type PassiveEffectType = genmodel.PassiveEffectType
type PlatformEffectType = genmodel.PlatformEffectType
type AttachmentEffectType = genmodel.AttachmentEffectType

// Re-export effect constants.
const (
	PassiveTPPerBackendDB      = genmodel.PassiveTPPerBackendDB
	PassiveTPPerBackendData    = genmodel.PassiveTPPerBackendData
	PassiveTPIfCardTypeOnField = genmodel.PassiveTPIfCardTypeOnField
	PassiveYieldPerOtherDB     = genmodel.PassiveYieldPerOtherDB
	PassiveYieldIfCardOnField  = genmodel.PassiveYieldIfCardOnField
	PassiveAVBonus             = genmodel.PassiveAVBonus
	PassiveScaleCostFree       = genmodel.PassiveScaleCostFree
	PlatformTPBonus            = genmodel.PlatformTPBonus
	PlatformYieldBonus         = genmodel.PlatformYieldBonus
	PlatformAVBonus            = genmodel.PlatformAVBonus
	AttachmentStatBonus        = genmodel.AttachmentStatBonus
)

// ── REST API contract types (from api package) ──

type PlayerResponse = apimodel.PlayerResponse
type BattleLimitResponse = apimodel.BattleLimitResponse
type ProductResponse = apimodel.ProductResponse
type UserSettings = apimodel.UserSettings
type EpisodeWithStatus = apimodel.EpisodeWithStatus
type LockReason = apimodel.LockReason
type NewsArticle = apimodel.NewsArticle
type Announcement = apimodel.Announcement
type DailyTip = apimodel.DailyTip
type SpectateGameInfo = apimodel.SpectateGameInfo
type DeckCardEntry = apimodel.DeckCardEntry
type DeckCreateRequest = apimodel.DeckCreateRequest
type DeckUpdateRequest = apimodel.DeckUpdateRequest
type UpdateSettingsRequest = apimodel.UpdateSettingsRequest
type PurchaseRequest = apimodel.PurchaseRequest
type RegisterRequest = apimodel.RegisterRequest
type SelectFactionRequest = apimodel.SelectFactionRequest

// ── DB models (locally defined) ──
// Player, PlayerDailyBattle, PlayerCard, GameConfig are in player.go.
