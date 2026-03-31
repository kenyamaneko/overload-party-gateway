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

// spectatorInfo holds a spectator's connection and the time they joined.
type spectatorInfo struct {
	conn     *Connection
	joinedAt time.Time
}

// gameInfo holds metadata about an active spectatable game.
type gameInfo struct {
	player1ID string
	player2ID string
	startedAt time.Time
}

// ActiveGameInfo is the public view of an active game, used for the REST API.
type ActiveGameInfo struct {
	GameID    string    `json:"game_id"`
	Player1ID string    `json:"player1_id"`
	Player2ID string    `json:"player2_id"`
	StartedAt time.Time `json:"started_at"`
}

// SpectateRelay manages spectator connections for active games.
// It is intentionally separate from GameRelay so that spectators
// never affect game membership or disconnect/forfeit logic.
type SpectateRelay struct {
	hub          *ConnectionHub
	battleClient service.BattleClient

	mu         sync.RWMutex
	// gameID → spectatorID → spectatorInfo
	spectators map[string]map[string]*spectatorInfo
	// gameID → game metadata
	games map[string]*gameInfo
}

func NewSpectateRelay(hub *ConnectionHub, battleClient service.BattleClient) *SpectateRelay {
	return &SpectateRelay{
		hub:          hub,
		battleClient: battleClient,
		spectators:   make(map[string]map[string]*spectatorInfo),
		games:        make(map[string]*gameInfo),
	}
}

// RegisterGame records the two players for a game so that spectate_update
// can later fetch a state view for player1 (as a canonical observer view).
func (sr *SpectateRelay) RegisterGame(gameID, player1ID, player2ID string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.games[gameID] = &gameInfo{
		player1ID: player1ID,
		player2ID: player2ID,
		startedAt: time.Now(),
	}
}

// UnregisterGame cleans up all spectator state for a finished game.
// It also sends spectate_ended to all current spectators.
func (sr *SpectateRelay) UnregisterGame(gameID string, winnerNum int64, winReason string) {
	sr.mu.Lock()
	spectatorMap := sr.spectators[gameID]
	delete(sr.spectators, gameID)
	delete(sr.games, gameID)
	sr.mu.Unlock()

	if len(spectatorMap) == 0 {
		return
	}

	msg := &WSMessage{
		Type: constants.WSMsgSpectateEnded,
		Data: mustMarshal(SpectateEndedMessage{
			GameID:    gameID,
			WinnerNum: winnerNum,
			WinReason: winReason,
		}),
	}
	for _, info := range spectatorMap {
		info.conn.SendMessage(msg)
	}
}

// HandleSpectateJoin processes a spectate_join message.
// It verifies the game exists via the battle server, then adds the spectator
// and responds with the current game state.
func (sr *SpectateRelay) HandleSpectateJoin(conn *Connection, data json.RawMessage) {
	var req SpectateJoinMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sr.sendSpectateError(conn, "invalid_data", "invalid spectate_join data")
		return
	}

	sr.mu.RLock()
	gi, knownGame := sr.games[req.GameID]
	sr.mu.RUnlock()

	if !knownGame || gi == nil {
		sr.sendSpectateError(conn, "game_not_found", "game not found or not active")
		return
	}

	// Fetch current game state for player1 as a canonical observer view.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rawState, err := sr.battleClient.GetGameStateForPlayer(ctx, req.GameID, gi.player1ID)
	if err != nil || rawState == nil {
		sr.sendSpectateError(conn, "state_unavailable", "could not retrieve game state")
		return
	}

	transformed, err := transformGameState(rawState)
	if err != nil {
		sr.sendSpectateError(conn, "state_unavailable", "could not transform game state")
		return
	}

	// Register spectator
	sr.mu.Lock()
	if sr.spectators[req.GameID] == nil {
		sr.spectators[req.GameID] = make(map[string]*spectatorInfo)
	}
	sr.spectators[req.GameID][conn.playerID] = &spectatorInfo{
		conn:     conn,
		joinedAt: time.Now(),
	}
	sr.mu.Unlock()

	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgSpectateJoined,
		Data: mustMarshal(SpectateJoinedMessage{
			GameID:    req.GameID,
			Player1ID: gi.player1ID,
			Player2ID: gi.player2ID,
			State:     transformed,
		}),
	})

	log.Printf("spectator %s joined game %s", conn.playerID, req.GameID)
}

// HandleSpectateLeave processes a spectate_leave message.
func (sr *SpectateRelay) HandleSpectateLeave(conn *Connection, data json.RawMessage) {
	var req SpectateLeaveMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}

	sr.mu.Lock()
	if m, ok := sr.spectators[req.GameID]; ok {
		delete(m, conn.playerID)
		if len(m) == 0 {
			delete(sr.spectators, req.GameID)
		}
	}
	sr.mu.Unlock()

	log.Printf("spectator %s left game %s", conn.playerID, req.GameID)
}

// RemoveSpectator removes a spectator from all games they may be watching.
// Called when their WebSocket connection is closed.
func (sr *SpectateRelay) RemoveSpectator(playerID string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	for gameID, m := range sr.spectators {
		if _, ok := m[playerID]; ok {
			delete(m, playerID)
			if len(m) == 0 {
				delete(sr.spectators, gameID)
			}
		}
	}
}

// HandleSpectateStamp processes a spectate_stamp message and broadcasts it.
func (sr *SpectateRelay) HandleSpectateStamp(conn *Connection, data json.RawMessage) {
	var req SpectateStampMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}

	msg := &WSMessage{
		Type: constants.WSMsgSpectateStampBroadcast,
		Data: mustMarshal(SpectateStampBroadcastMessage{
			GameID:      req.GameID,
			SpectatorID: conn.playerID,
			StampNo:     req.StampNo,
		}),
	}

	// Broadcast to all spectators of the game (including sender).
	sr.broadcastToSpectators(req.GameID, msg)
}

// BroadcastStateUpdate sends a spectate_update message to all spectators of a game.
// Called whenever game state changes (i.e. after game_state is sent to players).
func (sr *SpectateRelay) BroadcastStateUpdate(gameID string, state json.RawMessage) {
	msg := &WSMessage{
		Type: constants.WSMsgSpectateUpdate,
		Data: state,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("spectate: marshal update: %v", err)
		return
	}

	sr.mu.RLock()
	defer sr.mu.RUnlock()
	for _, info := range sr.spectators[gameID] {
		info.conn.SendRaw(data)
	}
}

// IsSpectator returns true if the given playerID is currently spectating any game.
func (sr *SpectateRelay) IsSpectator(playerID string) bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	for _, m := range sr.spectators {
		if _, ok := m[playerID]; ok {
			return true
		}
	}
	return false
}

func (sr *SpectateRelay) broadcastToSpectators(gameID string, msg *WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("spectate: marshal broadcast: %v", err)
		return
	}

	sr.mu.RLock()
	defer sr.mu.RUnlock()
	for _, info := range sr.spectators[gameID] {
		info.conn.SendRaw(data)
	}
}

// ActiveGames returns a snapshot of all currently registered games.
func (sr *SpectateRelay) ActiveGames() []ActiveGameInfo {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	result := make([]ActiveGameInfo, 0, len(sr.games))
	for gameID, gi := range sr.games {
		result = append(result, ActiveGameInfo{
			GameID:    gameID,
			Player1ID: gi.player1ID,
			Player2ID: gi.player2ID,
			StartedAt: gi.startedAt,
		})
	}
	return result
}

func (sr *SpectateRelay) sendSpectateError(conn *Connection, code, message string) {
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgSpectateError,
		Data: mustMarshal(SpectateErrorMessage{Code: code, Message: message}),
	})
}
