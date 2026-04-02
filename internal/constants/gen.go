// Re-export generated constants from the shared package.
// This allows all existing code to continue importing "internal/constants" unchanged.
package constants

import genconstants "github.com/kenyamaneko/overload-party-common/packages/gamedata/constants"

// Initial values.
const DeckSize = genconstants.DeckSize

// Factions.
const (
	FactionSHE     = genconstants.FactionSHE
	FactionTenki   = genconstants.FactionTenki
	FactionSugar   = genconstants.FactionSugar
	FactionTuners  = genconstants.FactionTuners
	FactionNeutral = genconstants.FactionNeutral
)

// SelectableFactions is the list of factions players can choose.
var SelectableFactions = genconstants.SelectableFactions

// Win reasons.
const (
	WinReasonBudgetZero    = genconstants.WinReasonBudgetZero
	WinReasonSystemDown    = genconstants.WinReasonSystemDown
	WinReasonRepositoryOut = genconstants.WinReasonRepositoryOut
	WinReasonTurnTimeout   = genconstants.WinReasonTurnTimeout
	WinReasonDisconnect    = genconstants.WinReasonDisconnect
	WinReasonTurnLimit     = genconstants.WinReasonTurnLimit
	WinReasonDraw          = genconstants.WinReasonDraw
	WinReasonLaunchFailure = genconstants.WinReasonLaunchFailure
	WinReasonSurrender     = genconstants.WinReasonSurrender
)

// Action types.
const (
	ActionTypeForfeit = genconstants.ActionTypeForfeit
)

// Event types.
const (
	EventTypeTurnStart   = genconstants.EventTypeTurnStart
	EventTypeBattleStart = genconstants.EventTypeBattleStart
)

// Match types.
const (
	MatchTypePvp = genconstants.MatchTypePvp
	MatchTypeNpc = genconstants.MatchTypeNpc
)

// WS server message types.
const (
	WSMsgGameState            = genconstants.WSServerMsgGameState
	WSMsgGameOver             = genconstants.WSServerMsgGameOver
	WSMsgError                = genconstants.WSServerMsgError
	WSMsgGameEntered          = genconstants.WSServerMsgGameEntered
	WSMsgMatchmakingStarted   = genconstants.WSServerMsgMatchmakingStarted
	WSMsgMatchmakingCancelled = genconstants.WSServerMsgMatchmakingCancelled
	WSMsgActionRejected       = genconstants.WSServerMsgActionRejected
	WSMsgStampUsed            = genconstants.WSServerMsgStampUsed
	WSMsgPong                 = genconstants.WSServerMsgPong
	WSMsgMatchFound           = genconstants.WSServerMsgMatchFound
	WSMsgActionPerformed      = genconstants.WSServerMsgActionPerformed
	WSMsgTurnControls         = genconstants.WSServerMsgTurnControls
	WSMsgNpcBattleCreated     = genconstants.WSServerMsgNpcBattleCreated
	WSMsgOpponentDisconnected = genconstants.WSServerMsgOpponentDisconnected
	WSMsgOpponentReconnected  = genconstants.WSServerMsgOpponentReconnected
	WSMsgSpectateJoined       = genconstants.WSServerMsgSpectateJoined
	WSMsgSpectateError        = genconstants.WSServerMsgSpectateError
	WSMsgSpectateUpdate       = genconstants.WSServerMsgSpectateUpdate
	WSMsgSpectateEnded        = genconstants.WSServerMsgSpectateEnded
	WSMsgSpectateStampBroadcast = genconstants.WSServerMsgSpectateStampBroadcast
	WSMsgGameStateRestore       = genconstants.WSServerMsgGameStateRestore
)

// WS client message types.
const (
	WSMsgGameEnter         = genconstants.WSClientMsgGameEnter
	WSMsgMatchmakingStart  = genconstants.WSClientMsgMatchmakingStart
	WSMsgMatchmakingCancel = genconstants.WSClientMsgMatchmakingCancel
	WSMsgGameAction        = genconstants.WSClientMsgGameAction
	WSMsgUseStamp          = genconstants.WSClientMsgUseStamp
	WSMsgPing              = genconstants.WSClientMsgPing
	WSMsgNpcBattleStart    = genconstants.WSClientMsgNpcBattleStart
	WSMsgSpectateJoin      = genconstants.WSClientMsgSpectateJoin
	WSMsgSpectateLeave     = genconstants.WSClientMsgSpectateLeave
	WSMsgSpectateStamp     = genconstants.WSClientMsgSpectateStamp
)

// Restriction helpers.
var RestrictionCopyCount = genconstants.RestrictionCopyCount
