package ws

import (
	"encoding/json"

	genmodel "github.com/kenyamaneko/overload-party-common/packages/go/model"
)

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// --- Client → Server ---

type MatchmakingStartMessage = genmodel.MatchmakingStartMessage
type GameEnterMessage = genmodel.GameEnterMessage
type NPCBattleStartMessage = genmodel.NPCBattleStartMessage
type GameActionMessage = genmodel.GameActionMessage
type UseStampMessage = genmodel.UseStampMessage

// --- Server → Client ---

type ErrorMessage = genmodel.ErrorMessage
type MatchFoundMessage = genmodel.MatchFoundMessage
type GameOverMessage = genmodel.GameOverMessage
type ActionRejectedMessage = genmodel.ActionRejectedMessage
type ActionPerformedMessage = genmodel.ActionPerformedMessage
type TurnControlsMessage = genmodel.TurnControlsMessage
type StampUsedMessage = genmodel.StampUsedMessage
type NPCBattleCreatedMessage = genmodel.NPCBattleCreatedMessage

// --- Spectate: Client → Server ---

type SpectateJoinMessage = genmodel.SpectateJoinMessage
type SpectateLeaveMessage = genmodel.SpectateLeaveMessage
type SpectateStampMessage = genmodel.SpectateStampMessage

// --- Spectate: Server → Client ---

type SpectateJoinedMessage = genmodel.SpectateJoinedMessage
type SpectateErrorMessage = genmodel.SpectateErrorMessage
type SpectateEndedMessage = genmodel.SpectateEndedMessage
type SpectateStampBroadcastMessage = genmodel.SpectateStampBroadcastMessage
