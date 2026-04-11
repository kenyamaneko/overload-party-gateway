// Re-export generated types from the shared package.
// This allows all existing code to continue importing "internal/model" unchanged.
package model

import (
	apiclient "github.com/kenyamaneko/overload-party-common/packages/api-client"
	cardtypes "github.com/kenyamaneko/overload-party-common/packages/card-types"
)

// ── Card / deck types ──

type CardDefinition = cardtypes.CardDefinition
type ComputeStats = cardtypes.ComputeStats
type DataStats = cardtypes.DataStats
type PlayerCardWithDef = apiclient.PlayerCardWithDef
type Deck = apiclient.Deck
type DeckCard = apiclient.DeckCard

// Type aliases for effect types.
type PassiveEffect = cardtypes.PassiveEffect
type PlatformEffect = cardtypes.PlatformEffect
type AttachmentEffect = cardtypes.AttachmentEffect
type PassiveEffectConfig = cardtypes.PassiveEffectConfig
type PlatformEffectConfig = cardtypes.PlatformEffectConfig
type AttachmentEffectConfig = cardtypes.AttachmentEffectConfig

// Type aliases for effect named types.
type PassiveEffectType = cardtypes.PassiveEffectType
type PlatformEffectType = cardtypes.PlatformEffectType
type AttachmentEffectType = cardtypes.AttachmentEffectType

// Re-export effect constants.
const (
	PassiveTPPerBackendDB      = cardtypes.PassiveTPPerBackendDB
	PassiveTPPerBackendData    = cardtypes.PassiveTPPerBackendData
	PassiveTPIfCardTypeOnField = cardtypes.PassiveTPIfCardTypeOnField
	PassiveYieldPerOtherDB     = cardtypes.PassiveYieldPerOtherDB
	PassiveYieldIfCardOnField  = cardtypes.PassiveYieldIfCardOnField
	PassiveAVBonus             = cardtypes.PassiveAVBonus
	PassiveScaleCostFree       = cardtypes.PassiveScaleCostFree
	PlatformTPBonus            = cardtypes.PlatformTPBonus
	PlatformYieldBonus         = cardtypes.PlatformYieldBonus
	PlatformAVBonus            = cardtypes.PlatformAVBonus
	AttachmentStatBonus        = cardtypes.AttachmentStatBonus
)

// ── REST API contract types (from api-client) ──

type PlayerResponse = apiclient.PlayerResponse
type BattleLimitResponse = apiclient.BattleLimitResponse
type ProductResponse = apiclient.ProductResponse
type UserSettings = apiclient.UserSettings
type EpisodeWithStatus = apiclient.EpisodeWithStatus
type LockReason = apiclient.LockReason
type NewsArticle = apiclient.NewsArticle
type Announcement = apiclient.Announcement
type DailyTip = apiclient.DailyTip
type SpectateGameInfo = apiclient.SpectateGameInfo
type DeckCardEntry = apiclient.DeckCardEntry
type DeckCreateRequest = apiclient.DeckCreateRequest
type DeckUpdateRequest = apiclient.DeckUpdateRequest
type UpdateSettingsRequest = apiclient.UpdateSettingsRequest
type PurchaseRequest = apiclient.PurchaseRequest
type RegisterRequest = apiclient.RegisterRequest
type SelectFactionRequest = apiclient.SelectFactionRequest

// ── DB models (locally defined) ──
// Player, PlayerDailyBattle, PlayerCard, GameConfig are in player.go.
