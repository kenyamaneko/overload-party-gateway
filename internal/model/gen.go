// Re-export generated types from the shared package.
// This allows all existing code to continue importing "internal/model" unchanged.
package model

import genmodel "github.com/kenyamaneko/overload-party-common/packages/gamedata/model"

// Type aliases for generated struct types.
type CardDefinition = genmodel.CardDefinition
type ComputeStats = genmodel.ComputeStats
type DataStats = genmodel.DataStats
type PlayerCard = genmodel.PlayerCard
type PlayerCardWithDef = genmodel.PlayerCardWithDef
type Deck = genmodel.Deck
type DeckCard = genmodel.DeckCard
type GameConfig = genmodel.GameConfig
type Player = genmodel.Player
type PlayerDailyBattle = genmodel.PlayerDailyBattle
type PassiveEffect = genmodel.PassiveEffect
type PlatformEffect = genmodel.PlatformEffect
type AttachmentEffect = genmodel.AttachmentEffect
type PassiveEffectConfig = genmodel.PassiveEffectConfig
type PlatformEffectConfig = genmodel.PlatformEffectConfig
type AttachmentEffectConfig = genmodel.AttachmentEffectConfig

// Type aliases for generated named types.
type PassiveEffectType = genmodel.PassiveEffectType
type PlatformEffectType = genmodel.PlatformEffectType
type AttachmentEffectType = genmodel.AttachmentEffectType

// Re-export generated constants.
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
