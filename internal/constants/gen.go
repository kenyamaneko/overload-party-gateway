// Re-export generated constants from the shared package.
// This allows all existing code to continue importing "internal/constants" unchanged.
package constants

import (
	gamedesign "github.com/kenyamaneko/overload-party-common/packages/gamedata/constants/game_design"
	gamelogic "github.com/kenyamaneko/overload-party-common/packages/gamedata/constants/game_logic"
	genws "github.com/kenyamaneko/overload-party-common/packages/gamedata/constants/ws"
)

// Initial values.
const DeckSize = gamedesign.DeckSize

// Factions.
const (
	FactionSHE     = gamedesign.FactionSHE
	FactionTenki   = gamedesign.FactionTenki
	FactionSugar   = gamedesign.FactionSugar
	FactionTuners  = gamedesign.FactionTuners
	FactionNeutral = gamedesign.FactionNeutral
)

// SelectableFactions is the list of factions players can choose.
var SelectableFactions = gamedesign.SelectableFactions

// Win reasons.
const (
	WinReasonBudgetZero    = gamelogic.WinReasonBudgetZero
	WinReasonSystemDown    = gamelogic.WinReasonSystemDown
	WinReasonRepositoryOut = gamelogic.WinReasonRepositoryOut
	WinReasonTurnTimeout   = gamelogic.WinReasonTurnTimeout
	WinReasonDisconnect    = gamelogic.WinReasonDisconnect
	WinReasonTurnLimit     = gamelogic.WinReasonTurnLimit
	WinReasonDraw          = gamelogic.WinReasonDraw
	WinReasonLaunchFailure = gamelogic.WinReasonLaunchFailure
	WinReasonSurrender     = gamelogic.WinReasonSurrender
)

// Action types.
const (
	ActionTypeForfeit = gamelogic.ActionTypeForfeit
)

// Event types.
const (
	EventTypeTurnStart   = gamelogic.EventTypeTurnStart
	EventTypeBattleStart = gamelogic.EventTypeBattleStart
)

// Match types.
const (
	MatchTypePvp = gamedesign.MatchTypePvp
	MatchTypeNpc = gamedesign.MatchTypeNpc
)

// WS server message types.
const (
	WSMsgGameState              = genws.WSServerMsgGameState
	WSMsgGameOver               = genws.WSServerMsgGameOver
	WSMsgError                  = genws.WSServerMsgError
	WSMsgGameEntered            = genws.WSServerMsgGameEntered
	WSMsgMatchmakingStarted     = genws.WSServerMsgMatchmakingStarted
	WSMsgMatchmakingCancelled   = genws.WSServerMsgMatchmakingCancelled
	WSMsgActionRejected         = genws.WSServerMsgActionRejected
	WSMsgStampUsed              = genws.WSServerMsgStampUsed
	WSMsgPong                   = genws.WSServerMsgPong
	WSMsgMatchFound             = genws.WSServerMsgMatchFound
	WSMsgActionPerformed        = genws.WSServerMsgActionPerformed
	WSMsgTurnControls           = genws.WSServerMsgTurnControls
	WSMsgNpcBattleCreated       = genws.WSServerMsgNpcBattleCreated
	WSMsgOpponentDisconnected   = genws.WSServerMsgOpponentDisconnected
	WSMsgOpponentReconnected    = genws.WSServerMsgOpponentReconnected
	WSMsgSpectateJoined         = genws.WSServerMsgSpectateJoined
	WSMsgSpectateError          = genws.WSServerMsgSpectateError
	WSMsgSpectateUpdate         = genws.WSServerMsgSpectateUpdate
	WSMsgSpectateEnded          = genws.WSServerMsgSpectateEnded
	WSMsgSpectateStampBroadcast = genws.WSServerMsgSpectateStampBroadcast
	WSMsgGameStateRestore       = genws.WSServerMsgGameStateRestore
)

// WS client message types.
const (
	WSMsgGameEnter         = genws.WSClientMsgGameEnter
	WSMsgMatchmakingStart  = genws.WSClientMsgMatchmakingStart
	WSMsgMatchmakingCancel = genws.WSClientMsgMatchmakingCancel
	WSMsgGameAction        = genws.WSClientMsgGameAction
	WSMsgUseStamp          = genws.WSClientMsgUseStamp
	WSMsgPing              = genws.WSClientMsgPing
	WSMsgNpcBattleStart    = genws.WSClientMsgNpcBattleStart
	WSMsgSpectateJoin      = genws.WSClientMsgSpectateJoin
	WSMsgSpectateLeave     = genws.WSClientMsgSpectateLeave
	WSMsgSpectateStamp     = genws.WSClientMsgSpectateStamp
)

// Restriction helpers.
var RestrictionCopyCount = gamedesign.RestrictionCopyCount
