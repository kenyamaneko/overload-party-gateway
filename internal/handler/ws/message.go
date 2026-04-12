package ws

import (
	"encoding/json"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

// WSMessage は全 WebSocket メッセージのエンベロープです
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// --- クライアント → サーバー ---

type MatchmakingStartMessage = apigateway.MatchmakingStartMessage
type GameEnterMessage = apigateway.GameEnterMessage
type NPCBattleStartMessage = apigateway.NPCBattleStartMessage
type GameActionMessage = apigateway.GameActionMessage
type UseStampMessage = apigateway.UseStampMessage

// --- サーバー → クライアント ---

type ErrorMessage = apigateway.ErrorMessage
type MatchFoundMessage = apigateway.MatchFoundMessage
type GameOverMessage = apigateway.GameOverMessage
type ActionRejectedMessage = apigateway.ActionRejectedMessage
type ActionPerformedMessage = apigateway.ActionPerformedMessage
type StampUsedMessage = apigateway.StampUsedMessage
type NPCBattleCreatedMessage = apigateway.NPCBattleCreatedMessage

// --- 観戦: クライアント → サーバー ---

type SpectateJoinMessage = apigateway.SpectateJoinMessage
type SpectateLeaveMessage = apigateway.SpectateLeaveMessage
type SpectateStampMessage = apigateway.SpectateStampMessage

// --- 観戦: サーバー → クライアント ---

type SpectateJoinedMessage = apigateway.SpectateJoinedMessage
type SpectateErrorMessage = apigateway.SpectateErrorMessage
type SpectateEndedMessage = apigateway.SpectateEndedMessage
type SpectateStampBroadcastMessage = apigateway.SpectateStampBroadcastMessage
