package ws

import (
	"encoding/json"

	apimodel "github.com/kenyamaneko/overload-party-common/packages/api/model"
	gamedatamodel "github.com/kenyamaneko/overload-party-common/packages/gamedata/model"
)

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// --- Client → Server ---
// These types are unchanged between packages; use gamedata for stability.

type MatchmakingStartMessage = gamedatamodel.MatchmakingStartMessage
type GameEnterMessage = gamedatamodel.GameEnterMessage
type NPCBattleStartMessage = gamedatamodel.NPCBattleStartMessage
type GameActionMessage = gamedatamodel.GameActionMessage
type UseStampMessage = gamedatamodel.UseStampMessage

// --- Server → Client ---
// Updated types (WinningPlayerNum, PlayerNum) come from api/model.

type ErrorMessage = gamedatamodel.ErrorMessage
type MatchFoundMessage = apimodel.MatchFoundMessage
type GameOverMessage = apimodel.GameOverMessage
type ActionRejectedMessage = gamedatamodel.ActionRejectedMessage
type ActionPerformedMessage = gamedatamodel.ActionPerformedMessage
type StampUsedMessage = apimodel.StampUsedMessage
type NPCBattleCreatedMessage = apimodel.NPCBattleCreatedMessage

// --- Spectate: Client → Server ---

type SpectateJoinMessage = gamedatamodel.SpectateJoinMessage
type SpectateLeaveMessage = gamedatamodel.SpectateLeaveMessage
type SpectateStampMessage = gamedatamodel.SpectateStampMessage

// --- Spectate: Server → Client ---

type SpectateJoinedMessage = apimodel.SpectateJoinedMessage
type SpectateErrorMessage = gamedatamodel.SpectateErrorMessage
type SpectateEndedMessage = apimodel.SpectateEndedMessage
type SpectateStampBroadcastMessage = gamedatamodel.SpectateStampBroadcastMessage
