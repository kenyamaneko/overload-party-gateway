package ws

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// Manager routes incoming WebSocket messages and coordinates the Hub,
// GameRelay, SpectateRelay, and matchmaking queue. It contains no game or
// connection state itself.
type Manager struct {
	Hub      *ConnectionHub
	Relay    *GameRelay
	Spectate *SpectateRelay

	battleClient   service.BattleClient
	playerService  *service.PlayerService
	deckService    *service.DeckService
	deckRepo       port.DeckRepo
	gameConfigRepo port.GameConfigRepo
	gamePlayerRepo port.GamePlayerRepo
	queue          *repository.MatchmakingQueue
}

func NewManager(battleClient service.BattleClient, playerService *service.PlayerService, deckService *service.DeckService, deckRepo port.DeckRepo, gameConfigRepo port.GameConfigRepo, gamePlayerRepo port.GamePlayerRepo) *Manager {
	queue := repository.NewMatchmakingQueue()

	m := &Manager{
		battleClient:   battleClient,
		playerService:  playerService,
		deckService:    deckService,
		deckRepo:       deckRepo,
		gameConfigRepo: gameConfigRepo,
		gamePlayerRepo: gamePlayerRepo,
		queue:          queue,
	}

	// Hub needs to query GameRelay for the player's gameID on disconnect,
	// and GameRelay needs Hub for sending messages. We wire them up here.
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:             func(playerID string) (string, bool) { return m.Relay.GameIDForPlayer(playerID) },
		OnDisconnectTimeout:   func(playerID, gameID string) { m.Relay.HandleDisconnectTimeout(playerID, gameID) },
		OnSpectatorDisconnect: func(playerID string) { m.Spectate.RemoveSpectator(playerID) },
		OnMatchmakingLeave:    func(playerID string) { m.queue.Leave(playerID) },
		OnGameDisconnect:      func(playerID, gameID string) { m.Relay.NotifyOpponentDisconnected(playerID, gameID) },
		OnGameReconnect:       func(playerID, gameID string) { m.Relay.NotifyOpponentReconnected(playerID, gameID) },
	})
	relay := NewGameRelay(hub, battleClient)
	spectate := NewSpectateRelay(hub, battleClient)

	m.Hub = hub
	m.Relay = relay
	m.Spectate = spectate

	// Cross-wire: GameRelay notifies SpectateRelay on state updates and game over.
	relay.spectateRelay = spectate

	// Wire player service for exp awarding on game over.
	relay.playerService = playerService

	// Wire player lookup for battle_start banner data.
	if playerService != nil {
		lookupFn := PlayerLookupFunc(func(ctx context.Context, playerID string) (string, int64, error) {
			p, err := playerService.GetPlayer(ctx, playerID)
			if err != nil {
				return "", 0, err
			}
			if p == nil {
				return "", 0, nil
			}
			return p.Username, p.Level, nil
		})
		relay.playerLookup = lookupFn
		spectate.playerLookup = lookupFn
	}

	return m
}

// StartMatchmaking starts the matchmaking loop. Should be called in a goroutine.
func (m *Manager) StartMatchmaking(ctx context.Context) {
	matcher := service.NewMatchmakingService(m.queue, func(ctx context.Context, result model.MatchResult) {
		notifyError := func(msg string) {
			errMsg := &WSMessage{
				Type: constants.WSMsgError,
				Data: mustMarshal(ErrorMessage{Code: "matchmaking_error", Message: msg, Retryable: true}),
			}
			for _, pid := range []string{result.Player1ID, result.Player2ID} {
				m.Hub.SendToPlayer(pid, errMsg)
			}
		}

		p1Cards, err := m.resolveDeckCards(ctx, result.Player1ID, result.Player1Deck)
		if err != nil {
			log.Printf("matchmaking: resolve p1 deck failed: %v", err)
			notifyError("failed to create game")
			return
		}
		p2Cards, err := m.resolveDeckCards(ctx, result.Player2ID, result.Player2Deck)
		if err != nil {
			log.Printf("matchmaking: resolve p2 deck failed: %v", err)
			notifyError("failed to create game")
			return
		}
		game, err := m.battleClient.CreatePvPGame(ctx, p1Cards, p2Cards)
		if err != nil {
			log.Printf("matchmaking: create pvp game failed: %v", err)
			notifyError("failed to create game")
			return
		}
		// game_players: ゲートウェイ管轄のプレイヤー ID マッピングを永続化
		if m.gamePlayerRepo != nil {
			if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 1, result.Player1ID); err != nil {
				log.Printf("matchmaking: insert game_player p1: %v", err)
			}
			if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 2, result.Player2ID); err != nil {
				log.Printf("matchmaking: insert game_player p2: %v", err)
			}
		}
		m.Relay.RegisterGameMeta(game.GameID, result.Player1ID, result.Player2ID, constants.MatchTypePvp)
		m.Spectate.RegisterGame(game.GameID, result.Player1ID, result.Player2ID)
		m.Relay.NotifyMatchFound(game.GameID, result.Player1ID, result.Player2ID)
	})
	matcher.Run(ctx)
}

// HandleMessage routes an incoming WebSocket message to the appropriate handler.
// Each message gets a 30-second timeout to prevent goroutine leaks from slow
// DB queries or battle server HTTP calls.
func (m *Manager) HandleMessage(conn *Connection, msg *WSMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
		// Spectators must not be able to send game actions.
		if m.Spectate.IsSpectator(conn.playerID) {
			log.Printf("spectator %s tried to send game_action — ignored", conn.playerID)
			return
		}
		m.Relay.HandleGameAction(ctx, conn, msg.Data)

	case constants.WSMsgUseStamp:
		m.Relay.HandleUseStamp(conn, msg.Data)

	case constants.WSMsgSpectateJoin:
		m.Spectate.HandleSpectateJoin(conn, msg.Data)

	case constants.WSMsgSpectateLeave:
		m.Spectate.HandleSpectateLeave(conn, msg.Data)

	case constants.WSMsgSpectateStamp:
		m.Spectate.HandleSpectateStamp(conn, msg.Data)

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
		sendError(conn, "invalid_data", "invalid matchmaking_start data", false)
		return
	}

	if msg, err := m.checkAndIncrementBattleLimit(ctx, conn.playerID); err != nil {
		sendError(conn, "matchmaking_error", err.Error(), false)
		return
	} else if msg != "" {
		sendError(conn, "matchmaking_error", msg, false)
		return
	}

	if err := m.deckService.ValidateDeckForBattle(ctx, conn.playerID, req.DeckID); err != nil {
		sendError(conn, "matchmaking_error", "deck validation failed: "+err.Error(), false)
		return
	}

	if err := m.queue.Join(conn.playerID, req.DeckID); err != nil {
		sendError(conn, "matchmaking_error", err.Error(), true)
		return
	}
	conn.SendMessage(&WSMessage{Type: constants.WSMsgMatchmakingStarted})
}

func (m *Manager) handleNpcBattleStart(ctx context.Context, conn *Connection, data json.RawMessage) {
	var req NPCBattleStartMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sendError(conn, "invalid_data", "invalid npc_battle_start data", false)
		return
	}

	if msg, err := m.checkAndIncrementBattleLimit(ctx, conn.playerID); err != nil {
		sendError(conn, "npc_battle_error", err.Error(), false)
		return
	} else if msg != "" {
		sendError(conn, "npc_battle_error", msg, false)
		return
	}

	if err := m.deckService.ValidateDeckForBattle(ctx, conn.playerID, req.DeckID); err != nil {
		sendError(conn, "npc_battle_error", "deck validation failed: "+err.Error(), false)
		return
	}

	cards, err := m.resolveDeckCards(ctx, conn.playerID, req.DeckID)
	if err != nil {
		sendError(conn, "npc_battle_error", "failed to resolve deck", true)
		return
	}

	game, err := m.battleClient.StartNPCBattle(ctx, cards, req.NPCModel)
	if err != nil {
		sendError(conn, "npc_battle_error", err.Error(), true)
		return
	}
	// NPC 戦では人間は常に player_num=1。NPC 側（player_num=2）は game_players に書かない。
	if m.gamePlayerRepo != nil {
		if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 1, conn.playerID); err != nil {
			log.Printf("npc battle: insert game_player: %v", err)
		}
	}
	m.Relay.RegisterGameMeta(game.GameID, conn.playerID, "", constants.MatchTypeNpc)
	m.Spectate.RegisterGame(game.GameID, conn.playerID, "")
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgNpcBattleCreated,
		Data: mustMarshal(NPCBattleCreatedMessage{
			GameID: game.GameID,
		}),
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
			cards = append(cards, service.BattleDeckCard{CardID: dc.CardID, ArtNo: dc.ArtNo})
		}
	}
	return cards, nil
}

// ActiveSpectateGames returns the list of currently active games available for spectating.
func (m *Manager) ActiveSpectateGames() []model.SpectateGameInfo {
	return m.Spectate.ActiveGames()
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
