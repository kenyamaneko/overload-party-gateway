package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// GameRelay manages game membership and relays game actions/state between
// players and the battle server.
type GameRelay struct {
	hub          *ConnectionHub
	battleClient service.BattleClient

	mu          sync.RWMutex
	gameMembers map[string][]string // gameID → []playerID
	playerGames map[string]string   // playerID → gameID
}

func NewGameRelay(hub *ConnectionHub, battleClient service.BattleClient) *GameRelay {
	return &GameRelay{
		hub:          hub,
		battleClient: battleClient,
		gameMembers:  make(map[string][]string),
		playerGames:  make(map[string]string),
	}
}

// GameIDForPlayer returns the gameID for a player, if any.
func (r *GameRelay) GameIDForPlayer(playerID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	gid, ok := r.playerGames[playerID]
	return gid, ok
}

func (r *GameRelay) JoinGame(playerID, gameID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.playerGames[playerID] = gameID
	r.gameMembers[gameID] = appendUnique(r.gameMembers[gameID], playerID)
}

func (r *GameRelay) LeaveGame(playerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if gameID, ok := r.playerGames[playerID]; ok {
		delete(r.playerGames, playerID)
		r.gameMembers[gameID] = removeString(r.gameMembers[gameID], playerID)
		if len(r.gameMembers[gameID]) == 0 {
			delete(r.gameMembers, gameID)
		}
	}
}

// BroadcastToGame sends the same message to all players in a game.
// 全員に同一メッセージを送るため、一度だけ Marshal して使い回す。
func (r *GameRelay) BroadcastToGame(gameID string, msg *WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal broadcast message: %v", err)
		return
	}

	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	for _, pid := range players {
		r.hub.SendRawToPlayer(pid, data)
	}
}

func (r *GameRelay) SendGameStateToPlayers(gameID string) {
	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	ctx := context.Background()
	for _, pid := range players {
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get game state for player %s: %v", pid, err)
			continue
		}
		transformed, err := transformGameState(state)
		if err != nil {
			log.Printf("transform game state for player %s: %v", pid, err)
			continue
		}
		r.hub.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgGameState,
			Data: transformed,
		})
	}
}

func (r *GameRelay) SendTurnControlsToPlayers(gameID string) {
	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	ctx := context.Background()
	for _, pid := range players {
		tc, err := r.battleClient.GetTurnControlsForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get turn controls for player %s: %v", pid, err)
			continue
		}
		if tc == nil {
			continue
		}
		r.hub.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgTurnControls,
			Data: mustMarshal(TurnControlsMessage{
				CanEndPhase:     tc.CanEndPhase,
				DiscardRequired: tc.DiscardRequired,
			}),
		})
	}
}

func (r *GameRelay) broadcastGameOver(gameID string, winnerNum int64, reason string) {
	r.BroadcastToGame(gameID, &WSMessage{
		Type: constants.WSMsgGameOver,
		Data: mustMarshal(GameOverMessage{
			GameID:    gameID,
			WinnerNum: winnerNum,
			WinReason: reason,
		}),
	})
}

// HandleGameEnter processes a game_enter message.
func (r *GameRelay) HandleGameEnter(conn *Connection, data json.RawMessage) {
	var req GameEnterMessage
	if err := json.Unmarshal(data, &req); err != nil {
		r.sendError(conn, "invalid_data", "invalid game_enter data", false)
		return
	}

	r.JoinGame(conn.playerID, req.GameID)
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgGameEntered,
		Data: mustMarshal(map[string]string{"game_id": req.GameID}),
	})
	r.SendGameStateToPlayers(req.GameID)
	r.SendTurnControlsToPlayers(req.GameID)
}

// HandleGameAction processes a game_action message.
func (r *GameRelay) HandleGameAction(ctx context.Context, conn *Connection, data json.RawMessage) {
	var action GameActionMessage
	if err := json.Unmarshal(data, &action); err != nil {
		r.sendError(conn, "invalid_data", "invalid game_action data", false)
		return
	}

	result, err := r.battleClient.ProcessAction(ctx, action.GameID, conn.playerID, action.ActionType, action.Data)
	if err != nil {
		conn.SendMessage(&WSMessage{
			Type: constants.WSMsgActionRejected,
			Data: mustMarshal(ActionRejectedMessage{
				GameID:     action.GameID,
				ActionType: action.ActionType,
				Reason:     err.Error(),
			}),
		})
		return
	}

	r.SendGameStateToPlayers(action.GameID)
	r.SendTurnControlsToPlayers(action.GameID)

	if result != nil && result.GameOver {
		r.broadcastGameOver(action.GameID, result.WinnerNum, result.WinReason)

		r.mu.RLock()
		players := make([]string, len(r.gameMembers[action.GameID]))
		copy(players, r.gameMembers[action.GameID])
		r.mu.RUnlock()
		for _, pid := range players {
			r.LeaveGame(pid)
		}
	}
}

// HandleUseStamp processes a use_stamp message.
func (r *GameRelay) HandleUseStamp(conn *Connection, data json.RawMessage) {
	var req UseStampMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}
	r.BroadcastToGame(req.GameID, &WSMessage{
		Type: constants.WSMsgStampUsed,
		Data: mustMarshal(StampUsedMessage{
			GameID:   req.GameID,
			PlayerID: conn.playerID,
			StampNo:  req.StampNo,
		}),
	})
}

// HandleDisconnectTimeout processes a forfeit after disconnect timeout.
func (r *GameRelay) HandleDisconnectTimeout(playerID, gameID string) {
	ctx := context.Background()
	result, err := r.battleClient.ProcessAction(ctx, gameID, playerID, "forfeit", nil)
	if err != nil {
		log.Printf("forfeit error: %v", err)
		return
	}
	if result != nil && result.GameOver {
		r.broadcastGameOver(gameID, result.WinnerNum, constants.WinReasonDisconnect)
	}
}

// NotifyMatchFound sends match_found to both players.
func (r *GameRelay) NotifyMatchFound(gameID, player1ID, player2ID string) {
	msg := &WSMessage{
		Type: constants.WSMsgMatchFound,
		Data: mustMarshal(MatchFoundMessage{
			GameID:    gameID,
			Player1ID: player1ID,
			Player2ID: player2ID,
		}),
	}
	r.hub.SendToPlayer(player1ID, msg)
	r.hub.SendToPlayer(player2ID, msg)
}

func (r *GameRelay) sendError(conn *Connection, code, message string, retryable bool) {
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgError,
		Data: mustMarshal(ErrorMessage{Code: code, Message: message, Retryable: retryable}),
	})
}

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

func removeString(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
