package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

const disconnectTimeout = 60 * time.Second

type disconnectInfo struct {
	playerID string
	gameID   string
	timer    *time.Timer
}

// Manager manages WebSocket connections, game membership, and matchmaking.
// It delegates game logic to BattleClient (battle server REST API).
type Manager struct {
	mu          sync.RWMutex
	connections map[string]*Connection // playerID → Connection
	gameMembers map[string][]string    // gameID → []playerID
	playerGames map[string]string      // playerID → gameID
	disconnects map[string]*disconnectInfo

	battleClient  service.BattleClient
	playerService *service.PlayerService
	queue         *repository.MatchmakingQueue
}

func NewManager(battleClient service.BattleClient, playerService *service.PlayerService) *Manager {
	queue := repository.NewMatchmakingQueue()
	m := &Manager{
		connections:   make(map[string]*Connection),
		gameMembers:   make(map[string][]string),
		playerGames:   make(map[string]string),
		disconnects:   make(map[string]*disconnectInfo),
		battleClient:  battleClient,
		playerService: playerService,
		queue:         queue,
	}
	return m
}

// StartMatchmaking starts the matchmaking loop. Should be called in a goroutine.
func (m *Manager) StartMatchmaking(ctx context.Context) {
	matcher := service.NewMatchmakingService(m.queue, func(ctx context.Context, result model.MatchResult) {
		game, err := m.battleClient.CreatePvPGame(
			ctx,
			result.Player1ID, result.Player1Deck,
			result.Player2ID, result.Player2Deck,
		)
		if err != nil {
			log.Printf("matchmaking: create pvp game failed: %v", err)
			return
		}
		m.NotifyMatchFound(game.GameID, game.Player1ID, game.Player2ID)
	})
	matcher.Run(ctx)
}

// ─── Connection lifecycle ───────────────────────────────────────────

func (m *Manager) Register(conn *Connection) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.disconnects[conn.playerID]; ok {
		info.timer.Stop()
		delete(m.disconnects, conn.playerID)
		log.Printf("player %s reconnected", conn.playerID)
	}

	if old, ok := m.connections[conn.playerID]; ok {
		old.Close()
	}
	m.connections[conn.playerID] = conn
}

func (m *Manager) Unregister(conn *Connection) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.connections[conn.playerID]; !ok || existing != conn {
		return
	}
	delete(m.connections, conn.playerID)

	// Start disconnect timer if player is in a game
	if gameID, inGame := m.playerGames[conn.playerID]; inGame {
		timer := time.AfterFunc(disconnectTimeout, func() {
			m.handleDisconnectTimeout(conn.playerID, gameID)
		})
		m.disconnects[conn.playerID] = &disconnectInfo{
			playerID: conn.playerID,
			gameID:   gameID,
			timer:    timer,
		}
		log.Printf("player %s disconnected, %v timeout started", conn.playerID, disconnectTimeout)
	}
}

func (m *Manager) handleDisconnectTimeout(playerID, gameID string) {
	m.mu.Lock()
	delete(m.disconnects, playerID)
	m.mu.Unlock()

	log.Printf("player %s disconnect timeout expired for game %s, forfeit", playerID, gameID)

	ctx := context.Background()
	result, err := m.battleClient.ProcessAction(ctx, gameID, playerID, "forfeit", nil)
	if err != nil {
		log.Printf("forfeit error: %v", err)
		return
	}
	if result != nil && result.GameOver {
		m.broadcastGameOver(gameID, result.WinnerNum, constants.WinReasonDisconnect)
	}
}

// ─── Game membership ─────────────────────────────────────────────

func (m *Manager) JoinGame(playerID, gameID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.playerGames[playerID] = gameID
	m.gameMembers[gameID] = appendUnique(m.gameMembers[gameID], playerID)
}

func (m *Manager) LeaveGame(playerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if gameID, ok := m.playerGames[playerID]; ok {
		delete(m.playerGames, playerID)
		m.gameMembers[gameID] = removeString(m.gameMembers[gameID], playerID)
		if len(m.gameMembers[gameID]) == 0 {
			delete(m.gameMembers, gameID)
		}
	}
}

// ─── Sending ─────────────────────────────────────────────────────

func (m *Manager) SendToPlayer(playerID string, msg *WSMessage) {
	m.mu.RLock()
	conn, ok := m.connections[playerID]
	m.mu.RUnlock()
	if ok {
		conn.SendMessage(msg)
	}
}

func (m *Manager) BroadcastToGame(gameID string, msg *WSMessage) {
	m.mu.RLock()
	players := m.gameMembers[gameID]
	m.mu.RUnlock()

	for _, pid := range players {
		m.SendToPlayer(pid, msg)
	}
}

func (m *Manager) SendGameStateToPlayers(gameID string) {
	m.mu.RLock()
	players := m.gameMembers[gameID]
	m.mu.RUnlock()

	ctx := context.Background()
	for _, pid := range players {
		state, err := m.battleClient.GetGameStateForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get game state for player %s: %v", pid, err)
			continue
		}
		m.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgGameState,
			Data: state,
		})
	}
}

func (m *Manager) SendTurnControlsToPlayers(gameID string) {
	m.mu.RLock()
	players := m.gameMembers[gameID]
	m.mu.RUnlock()

	ctx := context.Background()
	for _, pid := range players {
		tc, err := m.battleClient.GetTurnControlsForPlayer(ctx, gameID, pid)
		if err != nil {
			log.Printf("get turn controls for player %s: %v", pid, err)
			continue
		}
		if tc == nil {
			continue
		}
		m.SendToPlayer(pid, &WSMessage{
			Type: constants.WSMsgTurnControls,
			Data: mustMarshal(TurnControlsMessage{
				CanEndPhase:     tc.CanEndPhase,
				DiscardRequired: tc.DiscardRequired,
			}),
		})
	}
}

func (m *Manager) broadcastGameOver(gameID string, winnerNum int64, reason string) {
	m.BroadcastToGame(gameID, &WSMessage{
		Type: constants.WSMsgGameOver,
		Data: mustMarshal(GameOverMessage{
			GameID:    gameID,
			WinnerNum: winnerNum,
			WinReason: reason,
		}),
	})
}

// NotifyMatchFound sends match_found to both players. Called by the matchmaking loop.
func (m *Manager) NotifyMatchFound(gameID, player1ID, player2ID string) {
	msg := &WSMessage{
		Type: constants.WSMsgMatchFound,
		Data: mustMarshal(MatchFoundMessage{
			GameID:    gameID,
			Player1ID: player1ID,
			Player2ID: player2ID,
		}),
	}
	m.SendToPlayer(player1ID, msg)
	m.SendToPlayer(player2ID, msg)
}

// ─── Message handling ─────────────────────────────────────────────

func (m *Manager) HandleMessage(conn *Connection, msg *WSMessage) {
	ctx := context.Background()

	switch msg.Type {
	case constants.WSMsgGameEnter:
		m.handleGameEnter(conn, msg.Data)

	case constants.WSMsgMatchmakingStart:
		m.handleMatchmakingStart(ctx, conn, msg.Data)

	case constants.WSMsgMatchmakingCancel:
		m.queue.Leave(conn.playerID)
		conn.SendMessage(&WSMessage{Type: constants.WSMsgMatchmakingCancelled})

	case constants.WSMsgNpcBattleStart:
		m.handleNpcBattleStart(ctx, conn, msg.Data)

	case constants.WSMsgGameAction:
		m.handleGameAction(ctx, conn, msg.Data)

	case constants.WSMsgUseStamp:
		m.handleUseStamp(conn, msg.Data)

	case constants.WSMsgPing:
		conn.SendMessage(&WSMessage{Type: constants.WSMsgPong})

	default:
		log.Printf("unhandled message type: %s from player %s", msg.Type, conn.playerID)
	}
}

func (m *Manager) handleGameEnter(conn *Connection, data json.RawMessage) {
	var req GameEnterMessage
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendError(conn, "invalid_data", "invalid game_enter data", false)
		return
	}

	m.JoinGame(conn.playerID, req.GameID)
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgGameEntered,
		Data: mustMarshal(map[string]string{"game_id": req.GameID}),
	})
	m.SendGameStateToPlayers(req.GameID)
	m.SendTurnControlsToPlayers(req.GameID)
}

// handleMatchmakingStart checks the battle limit and enqueues the player.
// バトル回数はここで記録・チェックする（battleサーバーは関知しない）。
func (m *Manager) handleMatchmakingStart(ctx context.Context, conn *Connection, data json.RawMessage) {
	var req MatchmakingStartMessage
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendError(conn, "invalid_data", "invalid matchmaking_start data", false)
		return
	}

	// Check and increment daily battle count before joining queue
	if m.playerService != nil {
		limitResp, err := m.playerService.GetBattleLimit(ctx, conn.playerID)
		if err != nil {
			m.sendError(conn, "matchmaking_error", err.Error(), false)
			return
		}
		if !limitResp.CanBattle {
			m.sendError(conn, "matchmaking_error", "daily battle limit reached", false)
			return
		}
		if err := m.playerService.IncrementBattleCount(ctx, conn.playerID); err != nil {
			m.sendError(conn, "matchmaking_error", err.Error(), false)
			return
		}
	}

	if err := m.queue.Join(conn.playerID, req.DeckID); err != nil {
		m.sendError(conn, "matchmaking_error", err.Error(), true)
		return
	}
	conn.SendMessage(&WSMessage{Type: constants.WSMsgMatchmakingStarted})
}

func (m *Manager) handleNpcBattleStart(ctx context.Context, conn *Connection, data json.RawMessage) {
	var req NPCBattleStartMessage
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendError(conn, "invalid_data", "invalid npc_battle_start data", false)
		return
	}

	// Check and increment daily battle count before starting NPC battle
	if m.playerService != nil {
		limitResp, err := m.playerService.GetBattleLimit(ctx, conn.playerID)
		if err != nil {
			m.sendError(conn, "npc_battle_error", err.Error(), false)
			return
		}
		if !limitResp.CanBattle {
			m.sendError(conn, "npc_battle_error", "daily battle limit reached", false)
			return
		}
		if err := m.playerService.IncrementBattleCount(ctx, conn.playerID); err != nil {
			m.sendError(conn, "npc_battle_error", err.Error(), false)
			return
		}
	}

	game, err := m.battleClient.StartNPCBattle(ctx, conn.playerID, req.DeckID, req.NPCFaction)
	if err != nil {
		m.sendError(conn, "npc_battle_error", err.Error(), true)
		return
	}
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgNpcBattleCreated,
		Data: mustMarshal(NPCBattleCreatedMessage{
			GameID:    game.GameID,
			Player1ID: game.Player1ID,
			Player2ID: game.Player2ID,
		}),
	})
}

func (m *Manager) handleGameAction(ctx context.Context, conn *Connection, data json.RawMessage) {
	var action GameActionMessage
	if err := json.Unmarshal(data, &action); err != nil {
		m.sendError(conn, "invalid_data", "invalid game_action data", false)
		return
	}

	result, err := m.battleClient.ProcessAction(ctx, action.GameID, conn.playerID, action.ActionType, action.Data)
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

	m.SendGameStateToPlayers(action.GameID)
	m.SendTurnControlsToPlayers(action.GameID)

	if result != nil && result.GameOver {
		m.broadcastGameOver(action.GameID, result.WinnerNum, result.WinReason)

		m.mu.RLock()
		players := m.gameMembers[action.GameID]
		m.mu.RUnlock()
		for _, pid := range players {
			m.LeaveGame(pid)
		}
	}
}

func (m *Manager) handleUseStamp(conn *Connection, data json.RawMessage) {
	var req UseStampMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}
	m.BroadcastToGame(req.GameID, &WSMessage{
		Type: constants.WSMsgStampUsed,
		Data: mustMarshal(StampUsedMessage{
			GameID:   req.GameID,
			PlayerID: conn.playerID,
			StampNo:  req.StampNo,
		}),
	})
}

func (m *Manager) sendError(conn *Connection, code, message string, retryable bool) {
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgError,
		Data: mustMarshal(ErrorMessage{Code: code, Message: message, Retryable: retryable}),
	})
}

// ─── Helpers ─────────────────────────────────────────────────────

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
