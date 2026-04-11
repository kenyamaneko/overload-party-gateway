package ws

import (
	"encoding/json"

	apiclient "github.com/kenyamaneko/overload-party-common/packages/api-client"
)

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// --- Client → Server ---

type MatchmakingStartMessage = apiclient.MatchmakingStartMessage
type GameEnterMessage = apiclient.GameEnterMessage
type NPCBattleStartMessage = apiclient.NPCBattleStartMessage
type GameActionMessage = apiclient.GameActionMessage
type UseStampMessage = apiclient.UseStampMessage

// --- Server → Client ---

type ErrorMessage = apiclient.ErrorMessage
type MatchFoundMessage = apiclient.MatchFoundMessage
type GameOverMessage = apiclient.GameOverMessage
type ActionRejectedMessage = apiclient.ActionRejectedMessage
type ActionPerformedMessage = apiclient.ActionPerformedMessage
type StampUsedMessage = apiclient.StampUsedMessage
type NPCBattleCreatedMessage = apiclient.NPCBattleCreatedMessage

// --- Spectate: Client → Server ---

type SpectateJoinMessage = apiclient.SpectateJoinMessage
type SpectateLeaveMessage = apiclient.SpectateLeaveMessage
type SpectateStampMessage = apiclient.SpectateStampMessage

// --- Spectate: Server → Client ---

type SpectateJoinedMessage = apiclient.SpectateJoinedMessage
type SpectateErrorMessage = apiclient.SpectateErrorMessage
type SpectateEndedMessage = apiclient.SpectateEndedMessage
type SpectateStampBroadcastMessage = apiclient.SpectateStampBroadcastMessage
