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

// battleStateMeta is a minimal projection of the battle server's game state
// JSON, used only to extract turn-related metadata (timer, active player).
// Gateway does NOT transform the full game state — it is passed through as-is.
type battleStateMeta struct {
	CurrentTurn int64 `json:"currentTurn"`
	IsMyTurn    bool  `json:"isMyTurn"`
	MyView      struct {
		TimeBank int64 `json:"timeBank"`
	} `json:"myView"`
}

// turnTimerInfo holds the state for an active turn timer.
type turnTimerInfo struct {
	timer          *time.Timer
	activePlayerID string
}

// gameMetaInfo stores per-game metadata set at creation time.
type gameMetaInfo struct {
	player1ID string
	player2ID string
	matchType string // "pvp" or "npc"
}

// PlayerLookupFunc resolves a player's display name and level by ID.
type PlayerLookupFunc func(ctx context.Context, playerID string) (name string, level int64, err error)

// GameRelay manages game membership and relays game actions/state between
// players and the battle server.
type GameRelay struct {
	hub          *ConnectionHub
	battleClient service.BattleClient

	// spectateRelay is wired in after construction to avoid circular dependencies.
	spectateRelay *SpectateRelay
	// playerLookup is wired in after construction by the Manager.
	playerLookup PlayerLookupFunc
	// playerService is wired in after construction for exp awarding.
	playerService *service.PlayerService

	mu          sync.RWMutex
	gameMembers map[string][]string    // gameID → []playerID
	playerGames map[string]string      // playerID → gameID
	gameMeta    map[string]gameMetaInfo // gameID → metadata

	timerMu    sync.Mutex
	turnTimers map[string]*turnTimerInfo // gameID → active turn timer

	expAwarded map[string]bool // gameID → already awarded
}

func NewGameRelay(hub *ConnectionHub, battleClient service.BattleClient) *GameRelay {
	return &GameRelay{
		hub:          hub,
		battleClient: battleClient,
		gameMembers:  make(map[string][]string),
		playerGames:  make(map[string]string),
		gameMeta:     make(map[string]gameMetaInfo),
		turnTimers:   make(map[string]*turnTimerInfo),
		expAwarded:   make(map[string]bool),
	}
}

// RegisterGameMeta stores player IDs and match type for a game.
// Called at game creation time (matchmaking result or NPC battle start).
func (r *GameRelay) RegisterGameMeta(gameID, player1ID, player2ID, matchType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gameMeta[gameID] = gameMetaInfo{
		player1ID: player1ID,
		player2ID: player2ID,
		matchType: matchType,
	}
}

// GameIDForPlayer returns the gameID for a player, if any.
func (r *GameRelay) GameIDForPlayer(playerID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	gid, ok := r.playerGames[playerID]
	return gid, ok
}

// NotifyOpponentDisconnected sends an opponent_disconnected message to the
// other player in the game when a player disconnects.
func (r *GameRelay) NotifyOpponentDisconnected(playerID, gameID string) {
	r.sendToOpponent(playerID, gameID, constants.WSMsgOpponentDisconnected)
}

// NotifyOpponentReconnected sends an opponent_reconnected message to the
// other player when a disconnected player returns.
func (r *GameRelay) NotifyOpponentReconnected(playerID, gameID string) {
	r.sendToOpponent(playerID, gameID, constants.WSMsgOpponentReconnected)
}

// sendToOpponent resolves the opponent for a given player in a game and sends
// a message of the specified type.
func (r *GameRelay) sendToOpponent(playerID, gameID, msgType string) {
	r.mu.RLock()
	meta, ok := r.gameMeta[gameID]
	r.mu.RUnlock()
	if !ok {
		return
	}

	opponentID := meta.player2ID
	if playerID == meta.player2ID {
		opponentID = meta.player1ID
	}
	r.hub.SendToPlayer(opponentID, &WSMessage{Type: msgType})
}

func (r *GameRelay) JoinGame(playerID, gameID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if oldGameID, ok := r.playerGames[playerID]; ok && oldGameID != gameID {
		r.gameMembers[oldGameID] = removeString(r.gameMembers[oldGameID], playerID)
		if len(r.gameMembers[oldGameID]) == 0 {
			delete(r.gameMembers, oldGameID)
			delete(r.gameMeta, oldGameID)
		}
	}

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
			delete(r.gameMeta, gameID)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, pid := range players {
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get game state for player %s: %v", pid, err)
			sendErrorToPlayer(r.hub, pid, "game_state_error", "failed to retrieve game state", true)
			continue
		}

		// Extract turn timer info from the raw battle state
		var meta battleStateMeta
		if err := json.Unmarshal(state, &meta); err != nil {
			log.Printf("failed to extract turn timer for player %s: %v", pid, err)
		} else if meta.IsMyTurn {
			activePlayerID = pid
			activeTimeBank = meta.MyView.TimeBank
		}

		r.hub.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgGameState,
			Data: state,
		})

		// Capture the first player's state for spectators.
		if i == 0 {
			spectateState = state
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pid := range players {
		raw, err := r.battleClient.GetTurnControlsForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get turn controls for player %s: %v", pid, err)
			sendErrorToPlayer(r.hub, pid, "turn_controls_error", "failed to retrieve turn controls", true)
			continue
		}
		if raw == nil {
			continue
		}
		r.hub.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgTurnControls,
			Data: raw,
		})
	}
}

// sendActionPerformed dispatches action_performed messages for each event.
//
// Routing is based on who performed the action (event.PlayerID):
//   - Player's own action  → sent to opponents (gateway fetches their info-hidden state)
//   - Other player's action (NPC) → sent to the acting player (state included from battle server)
func (r *GameRelay) sendActionPerformed(gameID, actingPlayerID string, result *service.ActionResult) {
	if result == nil || len(result.Events) == 0 {
		return
	}

	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, evt := range result.Events {
		switch {
		case evt.PlayerID == "":
			// System event (turn_start etc.) → send to ALL players
			r.sendActionToPlayers(ctx, gameID, players, evt)

		case evt.PlayerID == actingPlayerID:
			// Player's own action → send to opponents
			opponents := make([]string, 0, len(players))
			for _, pid := range players {
				if pid != actingPlayerID {
					opponents = append(opponents, pid)
				}
			}
			r.sendActionToPlayers(ctx, gameID, opponents, evt)

		default:
			// Other player's action (NPC) → send to the acting player
			// State snapshot is passed through from the battle server as-is.
			if len(evt.State) == 0 {
				continue
			}
			r.hub.SendToPlayer(actingPlayerID, &WSMessage{
				Type: constants.WSMsgActionPerformed,
				Data: mustMarshal(ActionPerformedMessage{
					Sequence:   evt.Sequence,
					ActionType: evt.EventType,
					ActionData: evt.EventData,
					State:      evt.State,
				}),
			})
		}
	}
}

// sendActionToPlayers fetches each player's game state and sends the
// action_performed message. The state is passed through from the battle
// server without transformation.
func (r *GameRelay) sendActionToPlayers(ctx context.Context, gameID string, pids []string, evt service.ActionEvent) {
	for _, pid := range pids {
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get game state for action_performed (player %s): %v", pid, err)
			continue
		}
		r.hub.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgActionPerformed,
			Data: mustMarshal(ActionPerformedMessage{
				Sequence:   evt.Sequence,
				ActionType: evt.EventType,
				ActionData: evt.EventData,
				State:      state,
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

	r.awardGameExp(gameID, winnerNum, reason)

	// Notify and clean up spectators.
	if r.spectateRelay != nil {
		r.spectateRelay.UnregisterGame(gameID, winnerNum, reason)
	}
}

// awardGameExp grants experience points to players after a game ends.
// This runs after the game-over broadcast, so errors do not block the
// game-over flow. If config retrieval or DB update fails, exp awarding
// is skipped entirely and the error is logged.
func (r *GameRelay) awardGameExp(gameID string, winnerNum int64, reason string) {
	if r.playerService == nil {
		return
	}

	r.mu.Lock()
	if r.expAwarded[gameID] {
		r.mu.Unlock()
		return
	}
	r.expAwarded[gameID] = true
	meta, hasMeta := r.gameMeta[gameID]
	r.mu.Unlock()
	if !hasMeta {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := r.playerService.AwardGameExp(ctx, meta.player1ID, meta.player2ID, winnerNum, reason, meta.matchType); err != nil {
		log.Printf("ERROR: award game exp for game %s: %v", gameID, err)
	}
}

// HandleGameEnter processes a game_enter message.
func (r *GameRelay) HandleGameEnter(conn *Connection, data json.RawMessage) {
	var req GameEnterMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sendError(conn, "invalid_data", "invalid game_enter data", false)
		return
	}

	r.JoinGame(conn.playerID, req.GameID)
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgGameEntered,
		Data: mustMarshal(map[string]string{"game_id": req.GameID}),
	})

	// battle_start/turn_start carry the initial state for entry animation.
	// SendGameStateToPlayers follows to deliver the authoritative state
	// and start the turn timer. Clients use the two messages for different purposes.
	r.sendBattleStartAndTurnStart(conn, req.GameID)
	r.advanceNpcIfNeeded(req.GameID, conn.playerID)
	r.SendGameStateToPlayers(req.GameID)
	r.SendTurnControlsToPlayers(req.GameID)
}

// advanceNpcIfNeeded triggers the NPC's first turn if the NPC is the active
// player. This ensures NPC action events are delivered after game_enter,
// using the same flow as subsequent NPC turns.
func (r *GameRelay) advanceNpcIfNeeded(gameID, playerID string) {
	r.mu.RLock()
	meta, hasMeta := r.gameMeta[gameID]
	r.mu.RUnlock()

	if !hasMeta || meta.matchType != constants.MatchTypeNpc {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := r.battleClient.AdvanceNpcTurn(ctx, gameID, playerID)
	if err != nil {
		log.Printf("advance NPC turn (game %s): %v", gameID, err)
		return
	}

	r.sendActionPerformed(gameID, playerID, result)

	if result != nil && result.GameOver {
		r.cancelTurnTimer(gameID)
		r.broadcastGameOver(gameID, result.WinnerNum, result.WinReason)
		r.leaveAllPlayers(gameID)
	}
}

// sendBattleStartAndTurnStart sends battle_start and turn_start action_performed
// events to the entering player. These are synthetic events generated by the
// gateway (not from the battle server) because they require player profile data.
func (r *GameRelay) sendBattleStartAndTurnStart(conn *Connection, gameID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Fetch initial game state for this player
	rawState, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, conn.playerID)
	if err != nil {
		log.Printf("get game state for battle_start (player %s): %v", conn.playerID, err)
		sendErrorToPlayer(r.hub, conn.playerID, "game_state_error", "failed to retrieve game state", true)
		return
	}

	// Resolve game metadata
	r.mu.RLock()
	gameMeta, hasMeta := r.gameMeta[gameID]
	r.mu.RUnlock()

	// Build battle_start action_data
	battleStartData := map[string]interface{}{
		"match_type": constants.MatchTypePvp,
	}
	if hasMeta {
		battleStartData["match_type"] = gameMeta.matchType

		myName, myLevel := r.lookupPlayer(ctx, conn.playerID)
		var oppName string
		var oppLevel int64
		if gameMeta.matchType == constants.MatchTypeNpc {
			oppName, oppLevel = "NPC", 0
		} else {
			opponentID := gameMeta.player2ID
			if conn.playerID == gameMeta.player2ID {
				opponentID = gameMeta.player1ID
			}
			oppName, oppLevel = r.lookupPlayer(ctx, opponentID)
		}

		battleStartData["my_name"] = myName
		battleStartData["my_level"] = myLevel
		battleStartData["opponent_name"] = oppName
		battleStartData["opponent_level"] = oppLevel
	}

	// Send battle_start.
	// Sequence is 0 because battle_start and turn_start are synthetic gateway
	// events, not part of the battle server's event sequence.
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgActionPerformed,
		Data: mustMarshal(ActionPerformedMessage{
			Sequence:   0,
			ActionType: constants.EventTypeBattleStart,
			ActionData: mustMarshal(battleStartData),
			State:      rawState,
		}),
	})

	// Build turn_start action_data from game state
	var stateMeta battleStateMeta
	if err := json.Unmarshal(rawState, &stateMeta); err == nil {
		turnStartData := map[string]interface{}{
			"turn":       stateMeta.CurrentTurn,
			"is_my_turn": stateMeta.IsMyTurn,
		}
		conn.SendMessage(&WSMessage{
			Type: constants.WSMsgActionPerformed,
			Data: mustMarshal(ActionPerformedMessage{
				Sequence:   0,
				ActionType: constants.EventTypeTurnStart,
				ActionData: mustMarshal(turnStartData),
				State:      rawState,
			}),
		})
	}
}

// lookupPlayer resolves a player's display name and level.
// Returns defaults on error.
func (r *GameRelay) lookupPlayer(ctx context.Context, playerID string) (string, int64) {
	if r.playerLookup == nil {
		return "", 0
	}
	name, level, err := r.playerLookup(ctx, playerID)
	if err != nil {
		log.Printf("lookup player %s: %v", playerID, err)
		return "", 0
	}
	return name, level
}

// HandleGameAction processes a game_action message.
func (r *GameRelay) HandleGameAction(ctx context.Context, conn *Connection, data json.RawMessage) {
	var action GameActionMessage
	if err := json.Unmarshal(data, &action); err != nil {
		sendError(conn, "invalid_data", "invalid game_action data", false)
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

	// action_performed carries the state snapshot for client-side animation.
	// SendGameStateToPlayers follows to deliver the authoritative final state
	// and reset the turn timer. Clients use the two messages for different purposes.
	r.sendActionPerformed(action.GameID, conn.playerID, result)
	r.SendGameStateToPlayers(action.GameID)
	r.SendTurnControlsToPlayers(action.GameID)

	if result != nil && result.GameOver {
		r.cancelTurnTimer(action.GameID)
		r.broadcastGameOver(action.GameID, result.WinnerNum, result.WinReason)
		r.leaveAllPlayers(action.GameID)
	}
}

// leaveAllPlayers removes all players from a game's membership.
// Used after game over to clean up gameMembers / playerGames.
func (r *GameRelay) leaveAllPlayers(gameID string) {
	r.mu.RLock()
	players := make([]string, len(r.gameMembers[gameID]))
	copy(players, r.gameMembers[gameID])
	r.mu.RUnlock()
	for _, pid := range players {
		r.LeaveGame(pid)
	}

	r.mu.Lock()
	delete(r.expAwarded, gameID)
	r.mu.Unlock()
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
		info, ok := r.turnTimers[gameID]
		if !ok || info.activePlayerID != activePlayerID {
			// ターン交代済み — 旧プレイヤーへの誤 forfeit を防止
			r.timerMu.Unlock()
			return
		}
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
//
// forfeit reason は本来バトルのドメイン知識だが、ターンタイマーと切断タイマーは
// gateway の責務であり、Battle Server はタイムアウトの種別を区別できないため、
// 例外的に gateway が reason を指定して Battle Server に送る。
// broadcastGameOver でも gateway 側の WinReason で上書きしている。
func (r *GameRelay) handleTurnTimeout(gameID, playerID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := r.battleClient.ProcessAction(ctx, gameID, playerID, constants.ActionTypeForfeit, forfeitReason(constants.WinReasonTurnTimeout))
	if err != nil {
		log.Printf("turn timeout forfeit error (game=%s, player=%s): %v", gameID, playerID, err)
		return
	}
	if result != nil && result.GameOver {
		r.SendGameStateToPlayers(gameID)
		r.broadcastGameOver(gameID, result.WinnerNum, constants.WinReasonTurnTimeout)
		r.leaveAllPlayers(gameID)
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
// forfeit reason の方針については handleTurnTimeout のコメントを参照。
func (r *GameRelay) HandleDisconnectTimeout(playerID, gameID string) {
	r.cancelTurnTimer(gameID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := r.battleClient.ProcessAction(ctx, gameID, playerID, constants.ActionTypeForfeit, forfeitReason(constants.WinReasonDisconnect))
	if err != nil {
		log.Printf("forfeit error: %v", err)
		return
	}
	if result != nil && result.GameOver {
		r.broadcastGameOver(gameID, result.WinnerNum, constants.WinReasonDisconnect)
		r.leaveAllPlayers(gameID)
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

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// forfeitReason builds the action data for a forfeit request.
func forfeitReason(reason string) json.RawMessage {
	return mustMarshal(map[string]string{"Reason": reason})
}

func removeString(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
