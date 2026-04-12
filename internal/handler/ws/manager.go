package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// Manager は受信 WebSocket メッセージをルーティングし、Hub / GameRelay / SpectateRelay を調整します。
// Manager 自体はゲームや接続の状態を持たない。
type Manager struct {
	Hub      *ConnectionHub
	Relay    *GameRelay
	Spectate *SpectateRelay

	battleClient      service.BattleClient
	accountClient     *accountclient.Client
	cardClient        *cardclient.Client
	matchmakingClient *matchmakingclient.Client
	gamePlayerRepo    port.GamePlayerRepo

	// matchmaking_start 後のプレイヤー単位の待機タイムアウト。
	// 期限切れ時に matchmaking_error を push し上流をキャンセルする。
	matchmakingTimeout time.Duration

	// プレイヤー単位のマッチメイキングタイマー。HandleMatchMade で成功時に停止し、
	// 切断/キャンセル時にもクリアされる。
	matchWaitMu sync.Mutex
	matchWait   map[string]*time.Timer
}

// NewManager は WebSocket Manager を生成します
func NewManager(
	battleClient service.BattleClient,
	accountClient *accountclient.Client,
	cardClient *cardclient.Client,
	matchmakingClient *matchmakingclient.Client,
	gamePlayerRepo port.GamePlayerRepo,
	matchmakingTimeout time.Duration,
) *Manager {
	m := &Manager{
		battleClient:       battleClient,
		accountClient:      accountClient,
		cardClient:         cardClient,
		matchmakingClient:  matchmakingClient,
		gamePlayerRepo:     gamePlayerRepo,
		matchmakingTimeout: matchmakingTimeout,
		matchWait:          make(map[string]*time.Timer),
	}

	hub := NewConnectionHub(HubCallbacks{
		GetGameID:             func(playerID string) (string, bool) { return m.Relay.GameIDForPlayer(playerID) },
		OnDisconnectTimeout:   func(playerID, gameID string) { m.Relay.HandleDisconnectTimeout(playerID, gameID) },
		OnSpectatorDisconnect: func(playerID string) { m.Spectate.RemoveSpectator(playerID) },
		OnMatchmakingLeave:    m.cancelMatchmaking,
		OnGameDisconnect:      func(playerID, gameID string) { m.Relay.NotifyOpponentDisconnected(playerID, gameID) },
		OnGameReconnect:       func(playerID, gameID string) { m.Relay.NotifyOpponentReconnected(playerID, gameID) },
	})
	relay := NewGameRelay(hub, battleClient)
	spectate := NewSpectateRelay(hub, battleClient, gamePlayerRepo)

	m.Hub = hub
	m.Relay = relay
	m.Spectate = spectate

	relay.spectateRelay = spectate
	relay.accountClient = accountClient
	relay.gamePlayerRepo = gamePlayerRepo

	lookupFn := PlayerLookupFunc(func(ctx context.Context, playerID string) (string, int64, error) {
		p, err := accountClient.GetPlayer(ctx, playerID)
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

	return m
}

// cancelMatchmaking はプレイヤー切断時にマッチメイキングサービスへキャンセルを fire-and-forget する。
// Hub の unregister パスから呼ばれるためエラーはログのみ。
func (m *Manager) cancelMatchmaking(playerID string) {
	m.stopMatchWait(playerID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.matchmakingClient.Cancel(ctx, playerID); err != nil {
		log.Printf("matchmaking cancel for %s: %v", playerID, err)
	}
}

// startMatchWait はプレイヤー単位のマッチメイキングタイムアウトを開始（または再開始）する。
// タイマー発火時に matchmaking_error を push し上流をキャンセルする。
func (m *Manager) startMatchWait(playerID string) {
	if m.matchmakingTimeout <= 0 {
		return
	}
	m.matchWaitMu.Lock()
	if existing, ok := m.matchWait[playerID]; ok {
		existing.Stop()
	}
	t := time.AfterFunc(m.matchmakingTimeout, func() {
		m.handleMatchWaitTimeout(playerID)
	})
	m.matchWait[playerID] = t
	m.matchWaitMu.Unlock()
}

// stopMatchWait はプレイヤー単位のタイマーを停止する。冪等。
func (m *Manager) stopMatchWait(playerID string) {
	m.matchWaitMu.Lock()
	if t, ok := m.matchWait[playerID]; ok {
		t.Stop()
		delete(m.matchWait, playerID)
	}
	m.matchWaitMu.Unlock()
}

// handleMatchWaitTimeout は match_found 待ちが長すぎる場合に発火する。
// ローカルの待機エントリを削除し、matchmaking_error を push、上流をキャンセルする。
func (m *Manager) handleMatchWaitTimeout(playerID string) {
	m.matchWaitMu.Lock()
	if _, ok := m.matchWait[playerID]; !ok {
		m.matchWaitMu.Unlock()
		return
	}
	delete(m.matchWait, playerID)
	m.matchWaitMu.Unlock()

	log.Printf("matchmaking: wait timeout for player %s after %v", playerID, m.matchmakingTimeout)
	sendErrorToPlayer(m.Hub, playerID, "matchmaking_error", "matchmaking timed out", true)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.matchmakingClient.Cancel(ctx, playerID); err != nil {
		log.Printf("matchmaking: upstream cancel after timeout for %s: %v", playerID, err)
	}
}

// HandleMessage は受信 WebSocket メッセージを適切なハンドラーにルーティングします。
// goroutine リーク防止のため各メッセージに 30 秒のタイムアウトを設定する。
func (m *Manager) HandleMessage(conn *Connection, msg *WSMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch msg.Type {
	case constants.WSMsgGameEnter:
		m.Relay.HandleGameEnter(conn, msg.Data)

	case constants.WSMsgMatchmakingStart:
		m.handleMatchmakingStart(ctx, conn, msg.Data)

	case constants.WSMsgMatchmakingCancel:
		m.stopMatchWait(conn.playerID)
		if err := m.matchmakingClient.Cancel(ctx, conn.playerID); err != nil {
			sendError(conn, "matchmaking_error", "failed to cancel: "+err.Error(), true)
			return
		}
		conn.SendMessage(&WSMessage{Type: constants.WSMsgMatchmakingCancelled})

	case constants.WSMsgNpcBattleStart:
		m.handleNpcBattleStart(ctx, conn, msg.Data)

	case constants.WSMsgGameAction:
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

// handleMatchmakingStart はデッキを検証し（card サービス経由）、マッチメイキングサービスに enqueue する。
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

	if err := m.cardClient.ValidateDeckForBattle(ctx, conn.playerID, req.DeckID); err != nil {
		sendError(conn, "matchmaking_error", "deck validation failed: "+err.Error(), false)
		return
	}

	if err := m.matchmakingClient.Enqueue(ctx, conn.playerID, req.DeckID); err != nil {
		retryable := errors.Is(err, matchmakingclient.ErrUnavailable)
		sendError(conn, "matchmaking_error", "failed to enqueue: "+err.Error(), retryable)
		return
	}
	m.startMatchWait(conn.playerID)
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

	if err := m.cardClient.ValidateDeckForBattle(ctx, conn.playerID, req.DeckID); err != nil {
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
	if m.gamePlayerRepo != nil {
		if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 1, conn.playerID); err != nil {
			log.Printf("npc battle: insert game_player: %v", err)
		}
	}
	m.Spectate.RegisterGame(game.GameID)
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgNpcBattleCreated,
		Data: mustMarshal(NPCBattleCreatedMessage{
			GameID: game.GameID,
		}),
	})
}

// HandleMatchMade は port.MatchEventHandler の実装です。
// Pub/Sub subscriber が match_made イベント受信時に呼び出す。
//
// 全 Gateway Pod が competing-consumer で受信する。2 人のうちいずれかの
// WS 接続を保持する Pod のみが通知を push し、他の Pod は ack して終了する。
func (m *Manager) HandleMatchMade(ctx context.Context, event port.MatchMadeEvent) error {
	if len(event.Players) != 2 {
		return errors.New("match_made event must contain exactly 2 players")
	}

	// どの Pod が接続を保持するかに関わらず待機タイマーを停止（保持していない Pod では noop）
	m.stopMatchWait(event.Players[0].PlayerID)
	m.stopMatchWait(event.Players[1].PlayerID)

	p1Cards, err := m.resolveDeckCards(ctx, event.Players[0].PlayerID, event.Players[0].DeckID)
	if err != nil {
		return err
	}
	p2Cards, err := m.resolveDeckCards(ctx, event.Players[1].PlayerID, event.Players[1].DeckID)
	if err != nil {
		return err
	}

	game, err := m.battleClient.CreatePvPGame(ctx, p1Cards, p2Cards)
	if err != nil {
		return err
	}

	if m.gamePlayerRepo != nil {
		if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 1, event.Players[0].PlayerID); err != nil {
			log.Printf("match_made: insert game_player p1: %v", err)
		}
		if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 2, event.Players[1].PlayerID); err != nil {
			log.Printf("match_made: insert game_player p2: %v", err)
		}
	}

	m.Spectate.RegisterGame(game.GameID)
	m.Relay.NotifyMatchFound(game.GameID, event.Players[0].PlayerID, event.Players[1].PlayerID)
	return nil
}

func (m *Manager) resolveDeckCards(ctx context.Context, playerID string, deckID int64) ([]service.BattleDeckCard, error) {
	deckCards, err := m.cardClient.GetDeckCards(ctx, playerID, deckID)
	if err != nil {
		return nil, err
	}
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

// ActiveSpectateGames は現在観戦可能なゲーム一覧を返します
func (m *Manager) ActiveSpectateGames() []apigateway.SpectateGameInfo {
	return m.Spectate.ActiveGames()
}

func (m *Manager) checkAndIncrementBattleLimit(ctx context.Context, playerID string) (string, error) {
	if m.accountClient == nil {
		return "", nil
	}
	limitResp, err := m.accountClient.GetBattleLimit(ctx, playerID)
	if err != nil {
		return "", err
	}
	if !limitResp.CanBattle {
		return "daily battle limit reached", nil
	}
	if err := m.accountClient.IncrementBattleCount(ctx, playerID); err != nil {
		return "", err
	}
	return "", nil
}

// resolveDeckCards での DeckCard 間接参照のため import を維持
var _ = apigateway.DeckCard{}
