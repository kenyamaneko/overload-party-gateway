package ws

import (
	"context"
	"encoding/json"
	"log"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// Manager routes incoming WebSocket messages and coordinates the Hub,
// GameRelay, and matchmaking queue. It contains no game or connection
// state itself.
type Manager struct {
	Hub   *ConnectionHub
	Relay *GameRelay

	battleClient  service.BattleClient
	playerService *service.PlayerService
	deckRepo      repository.DeckRepo
	queue         *repository.MatchmakingQueue
}

func NewManager(battleClient service.BattleClient, playerService *service.PlayerService, deckRepo repository.DeckRepo) *Manager {
	queue := repository.NewMatchmakingQueue()

	m := &Manager{
		battleClient:  battleClient,
		playerService: playerService,
		deckRepo:      deckRepo,
		queue:         queue,
	}

	// Hub needs to query GameRelay for the player's gameID on disconnect,
	// and GameRelay needs Hub for sending messages. We wire them up here.
	hub := NewConnectionHub(
		func(playerID string) (string, bool) { return m.Relay.GameIDForPlayer(playerID) },
		func(playerID, gameID string) { m.Relay.HandleDisconnectTimeout(playerID, gameID) },
	)
	relay := NewGameRelay(hub, battleClient)

	m.Hub = hub
	m.Relay = relay
	return m
}

// StartMatchmaking starts the matchmaking loop. Should be called in a goroutine.
func (m *Manager) StartMatchmaking(ctx context.Context) {
	matcher := service.NewMatchmakingService(m.queue, func(ctx context.Context, result model.MatchResult) {
		p1Cards, err := m.resolveDeckCards(ctx, result.Player1ID, result.Player1Deck)
		if err != nil {
			log.Printf("matchmaking: resolve p1 deck failed: %v", err)
			return
		}
		p2Cards, err := m.resolveDeckCards(ctx, result.Player2ID, result.Player2Deck)
		if err != nil {
			log.Printf("matchmaking: resolve p2 deck failed: %v", err)
			return
		}
		game, err := m.battleClient.CreatePvPGame(
			ctx,
			result.Player1ID, result.Player1Deck, p1Cards,
			result.Player2ID, result.Player2Deck, p2Cards,
		)
		if err != nil {
			log.Printf("matchmaking: create pvp game failed: %v", err)
			return
		}
		m.Relay.NotifyMatchFound(game.GameID, game.Player1ID, game.Player2ID)
	})
	matcher.Run(ctx)
}

// HandleMessage routes an incoming WebSocket message to the appropriate handler.
func (m *Manager) HandleMessage(conn *Connection, msg *WSMessage) {
	ctx := context.Background()

	switch msg.Type {
	case constants.WSMsgGameEnter:
		m.Relay.HandleGameEnter(conn, msg.Data)

	case constants.WSMsgMatchmakingStart:
		m.handleMatchmakingStart(ctx, conn, msg.Data)

	case constants.WSMsgMatchmakingCancel:
		m.queue.Leave(conn.playerID)
		conn.SendMessage(&WSMessage{Type: constants.WSMsgMatchmakingCancelled})

	case constants.WSMsgNpcBattleStart:
		m.handleNpcBattleStart(ctx, conn, msg.Data)

	case constants.WSMsgGameAction:
		m.Relay.HandleGameAction(ctx, conn, msg.Data)

	case constants.WSMsgUseStamp:
		m.Relay.HandleUseStamp(conn, msg.Data)

	case constants.WSMsgPing:
		conn.SendMessage(&WSMessage{Type: constants.WSMsgPong})

	default:
		log.Printf("unhandled message type: %s from player %s", msg.Type, conn.playerID)
	}
}

// handleMatchmakingStart checks the battle limit and enqueues the player.
func (m *Manager) handleMatchmakingStart(ctx context.Context, conn *Connection, data json.RawMessage) {
	var req MatchmakingStartMessage
	if err := json.Unmarshal(data, &req); err != nil {
		m.sendError(conn, "invalid_data", "invalid matchmaking_start data", false)
		return
	}

	if msg, err := m.checkAndIncrementBattleLimit(ctx, conn.playerID); err != nil {
		m.sendError(conn, "matchmaking_error", err.Error(), false)
		return
	} else if msg != "" {
		m.sendError(conn, "matchmaking_error", msg, false)
		return
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

	if msg, err := m.checkAndIncrementBattleLimit(ctx, conn.playerID); err != nil {
		m.sendError(conn, "npc_battle_error", err.Error(), false)
		return
	} else if msg != "" {
		m.sendError(conn, "npc_battle_error", msg, false)
		return
	}

	cards, err := m.resolveDeckCards(ctx, conn.playerID, req.DeckID)
	if err != nil {
		m.sendError(conn, "npc_battle_error", "failed to resolve deck", true)
		return
	}

	game, err := m.battleClient.StartNPCBattle(ctx, conn.playerID, req.DeckID, cards, req.NPCFaction)
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

func (m *Manager) sendError(conn *Connection, code, message string, retryable bool) {
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgError,
		Data: mustMarshal(ErrorMessage{Code: code, Message: message, Retryable: retryable}),
	})
}

func (m *Manager) resolveDeckCards(ctx context.Context, playerID string, deckID int64) ([]service.BattleDeckCard, error) {
	deckCards, err := m.deckRepo.GetDeckCards(ctx, playerID, deckID)
	if err != nil {
		return nil, err
	}
	// DeckCardはカード種別ごとの行(Count>=1)なので、展開後の総枚数を先に求めて
	// スライスを事前確保し、ループ中の再割当てを防ぐ。
	totalCount := 0
	for _, dc := range deckCards {
		totalCount += dc.Count
	}
	cards := make([]service.BattleDeckCard, 0, totalCount)
	for _, dc := range deckCards {
		for i := 0; i < dc.Count; i++ {
			cards = append(cards, service.BattleDeckCard{CardNo: dc.CardNo, ArtNo: dc.ArtNo})
		}
	}
	return cards, nil
}

func (m *Manager) checkAndIncrementBattleLimit(ctx context.Context, playerID string) (string, error) {
	if m.playerService == nil {
		return "", nil
	}
	limitResp, err := m.playerService.GetBattleLimit(ctx, playerID)
	if err != nil {
		return "", err
	}
	if !limitResp.CanBattle {
		return "daily battle limit reached", nil
	}
	if err := m.playerService.IncrementBattleCount(ctx, playerID); err != nil {
		return "", err
	}
	return "", nil
}
