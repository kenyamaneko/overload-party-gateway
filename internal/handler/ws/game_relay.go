package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// turnTimerInfo holds the state for an active turn timer.
type turnTimerInfo struct {
	timer          *time.Timer
	activePlayerID string
}

// GameRelay manages game membership and relays game actions/state between
// players and the battle server.
type GameRelay struct {
	hub          *ConnectionHub
	battleClient service.BattleClient

	// spectateRelay is wired in after construction to avoid circular dependencies.
	spectateRelay *SpectateRelay

	mu          sync.RWMutex
	gameMembers map[string][]string // gameID → []playerID
	playerGames map[string]string   // playerID → gameID

	timerMu    sync.Mutex
	turnTimers map[string]*turnTimerInfo // gameID → active turn timer
}

func NewGameRelay(hub *ConnectionHub, battleClient service.BattleClient) *GameRelay {
	return &GameRelay{
		hub:          hub,
		battleClient: battleClient,
		gameMembers:  make(map[string][]string),
		playerGames:  make(map[string]string),
		turnTimers:   make(map[string]*turnTimerInfo),
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

	var activePlayerID string
	var activeTimeBank int64
	// spectateState holds the transformed state for the first player,
	// used as the canonical observer view sent to spectators.
	var spectateState json.RawMessage

	ctx := context.Background()
	for i, pid := range players {
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get game state for player %s: %v", pid, err)
			r.sendErrorToPlayer(pid, "game_state_error", "failed to retrieve game state", true)
			continue
		}

		// Extract turn timer info from the raw battle state
		var b battleGameState
		if json.Unmarshal(state, &b) == nil && b.IsMyTurn {
			activePlayerID = pid
			activeTimeBank = b.MyView.TimeBank
		}

		transformed, err := transformGameState(state)
		if err != nil {
			log.Printf("transform game state for player %s: %v", pid, err)
			r.sendErrorToPlayer(pid, "game_state_error", "failed to process game state", true)
			continue
		}
		r.hub.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgGameState,
			Data: transformed,
		})

		// Capture the first player's state for spectators.
		if i == 0 {
			spectateState = transformed
		}
	}

	// Refresh turn timer based on active player's TimeBank
	if activePlayerID != "" {
		r.resetTurnTimer(gameID, activePlayerID, activeTimeBank)
	}

	// Forward state updates to spectators.
	if r.spectateRelay != nil && spectateState != nil {
		r.spectateRelay.BroadcastStateUpdate(gameID, spectateState)
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
			r.sendErrorToPlayer(pid, "turn_controls_error", "failed to retrieve turn controls", true)
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

	// Notify and clean up spectators.
	if r.spectateRelay != nil {
		r.spectateRelay.UnregisterGame(gameID, winnerNum, reason)
	}
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
		r.cancelTurnTimer(action.GameID)
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

// resetTurnTimer cancels any existing timer for the game and starts a new one.
// When the timer fires, it sends a forfeit for the active player (timeout loss).
func (r *GameRelay) resetTurnTimer(gameID, activePlayerID string, timeBankSeconds int64) {
	r.timerMu.Lock()
	defer r.timerMu.Unlock()

	// Cancel existing timer
	if info, ok := r.turnTimers[gameID]; ok {
		info.timer.Stop()
		delete(r.turnTimers, gameID)
	}

	if timeBankSeconds <= 0 {
		return
	}

	// Add a small buffer (2s) to account for network latency.
	// The battle server is the authoritative source — if the player sends an
	// action just before the Gateway timer fires, the server will deduct the
	// real elapsed time and may still allow the action.
	duration := time.Duration(timeBankSeconds+2) * time.Second

	timer := time.AfterFunc(duration, func() {
		r.timerMu.Lock()
		delete(r.turnTimers, gameID)
		r.timerMu.Unlock()

		log.Printf("turn timer expired for game %s, player %s", gameID, activePlayerID)
		r.handleTurnTimeout(gameID, activePlayerID)
	})

	r.turnTimers[gameID] = &turnTimerInfo{
		timer:          timer,
		activePlayerID: activePlayerID,
	}
}

// cancelTurnTimer stops and removes the turn timer for a game.
func (r *GameRelay) cancelTurnTimer(gameID string) {
	r.timerMu.Lock()
	defer r.timerMu.Unlock()

	if info, ok := r.turnTimers[gameID]; ok {
		info.timer.Stop()
		delete(r.turnTimers, gameID)
	}
}

// handleTurnTimeout sends a forfeit action when the turn timer expires.
func (r *GameRelay) handleTurnTimeout(gameID, playerID string) {
	ctx := context.Background()
	result, err := r.battleClient.ProcessAction(ctx, gameID, playerID, "forfeit", nil)
	if err != nil {
		log.Printf("turn timeout forfeit error (game=%s, player=%s): %v", gameID, playerID, err)
		return
	}
	if result != nil && result.GameOver {
		r.SendGameStateToPlayers(gameID)
		r.broadcastGameOver(gameID, result.WinnerNum, result.WinReason)

		r.mu.RLock()
		players := make([]string, len(r.gameMembers[gameID]))
		copy(players, r.gameMembers[gameID])
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
	r.cancelTurnTimer(gameID)

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

func (r *GameRelay) sendErrorToPlayer(playerID, code, message string, retryable bool) {
	r.hub.SendToPlayer(playerID, &WSMessage{
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
