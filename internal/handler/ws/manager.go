package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// Manager は受信 WebSocket メッセージをルーティングし、Hub / GameRelay を調整します。
// Manager 自体はゲームや接続の状態を持たない。
type Manager struct {
	Hub   *ConnectionHub
	Relay *GameRelay

	battleClient      service.BattleClient
	accountClient     port.AccountClient
	cardClient        port.CardClient
	matchmakingClient port.MatchmakingClient
	gamePlayerRepo    port.GamePlayerRepo

	// internalSigner は WS 経路から各サービス client を呼ぶ前に X-Internal-Auth JWT を発行する。
	internalSigner *internalauth.Signer

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
	accountClient port.AccountClient,
	cardClient port.CardClient,
	matchmakingClient port.MatchmakingClient,
	gamePlayerRepo port.GamePlayerRepo,
	matchmakingTimeout time.Duration,
	internalSigner *internalauth.Signer,
) *Manager {
	m := &Manager{
		battleClient:       battleClient,
		accountClient:      accountClient,
		cardClient:         cardClient,
		matchmakingClient:  matchmakingClient,
		gamePlayerRepo:     gamePlayerRepo,
		internalSigner:     internalSigner,
		matchmakingTimeout: matchmakingTimeout,
		matchWait:          make(map[string]*time.Timer),
	}

	// HubCallbacks は m.Relay の後初期化を見込んで遅延参照する。
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(playerID string) (string, bool) { return m.Relay.GameIDForPlayer(playerID) },
		OnDisconnectTimeout: func(playerID, gameID string) { m.Relay.HandleDisconnectTimeout(playerID, gameID) },
		OnMatchmakingLeave:  m.cancelMatchmaking,
		OnGameDisconnect:    func(playerID, gameID string) { m.Relay.NotifyOpponentDisconnected(playerID, gameID) },
		OnGameReconnect:     func(playerID, gameID string) { m.Relay.NotifyOpponentReconnected(playerID, gameID) },
	})

	relay := NewGameRelay(hub, battleClient, accountClient, gamePlayerRepo)

	m.Hub = hub
	m.Relay = relay

	return m
}

// buildAuthedContext は ctx に内部認証 JWT を注入した派生 context を返す。
func (m *Manager) buildAuthedContext(ctx context.Context, playerID string) context.Context {
	token, err := m.internalSigner.Issue(playerID)
	if err != nil {
		// Issue は起動時に検証済の resolver と非空 playerID で必ず成功するため、ここでの失敗は
		// 起動時 invariants の違反 (programming error) を意味し panic で fail-fast する。
		panic(fmt.Errorf("internalauth: issue: %w", err))
	}
	return internalauth.WithToken(ctx, token)
}

// cancelMatchmaking はプレイヤー切断時にマッチメイキングサービスへキャンセルを fire-and-forget する。
// Hub の unregister パスから呼ばれる時点で WS コネクションは既に切断済みなので、
// 親 ctx は Background を使い、短いタイムアウトだけ付ける。エラーはログのみ。
func (m *Manager) cancelMatchmaking(playerID string) {
	m.stopMatchWait(playerID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.matchmakingClient.Cancel(ctx); err != nil {
		slog.Warn("matchmaking cancel failed", "player_id", playerID, "error", err)
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

	slog.Warn("matchmaking wait timeout", "player_id", playerID, "timeout", m.matchmakingTimeout)
	sendErrorToPlayer(m.Hub, playerID, "matchmaking_error", "matchmaking timed out", true)

	// タイマー発火経路は WS リクエスト ctx を持たない。上流キャンセルは接続状態に依存せず完了させたい。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := m.matchmakingClient.Cancel(ctx); err != nil {
		slog.Warn("matchmaking upstream cancel after timeout failed", "player_id", playerID, "error", err)
	}
}

// HandleMessage は受信 WebSocket メッセージを適切なハンドラーにルーティングします。
// 親 ctx は conn.Context()（WS 切断で cancel）とし、下流呼び出しが切断後も走り続けないようにする。
// 個別メッセージ処理の暴走を防ぐため 30 秒の上限を重ねる。
func (m *Manager) HandleMessage(conn *Connection, msg *WSMessage) {
	ctx, cancel := context.WithTimeout(conn.Context(), 30*time.Second)
	defer cancel()
	ctx = m.buildAuthedContext(ctx, conn.playerID)

	switch msg.Type {
	case genws.WSClientMsgGameEnter:
		m.Relay.HandleGameEnter(conn, msg.Data)

	case genws.WSClientMsgMatchmakingStart:
		m.handleMatchmakingStart(ctx, conn, msg.Data)

	case genws.WSClientMsgMatchmakingCancel:
		m.stopMatchWait(conn.playerID)
		if err := m.matchmakingClient.Cancel(ctx); err != nil {
			sendError(conn, "matchmaking_error", "failed to cancel: "+err.Error(), true)
			return
		}
		conn.SendMessage(&WSMessage{Type: genws.WSServerMsgMatchmakingCancelled})

	case genws.WSClientMsgNpcBattleStart:
		m.handleNpcBattleStart(ctx, conn, msg.Data)

	case genws.WSClientMsgGameAction:
		m.Relay.HandleGameAction(ctx, conn, msg.Data)

	case genws.WSClientMsgUseStamp:
		m.Relay.HandleUseStamp(conn, msg.Data)

	case genws.WSClientMsgPing:
		conn.SendMessage(&WSMessage{Type: genws.WSServerMsgPong})

	default:
		slog.Warn("unhandled message type", "type", msg.Type, "player_id", conn.playerID)
	}
}

// handleMatchmakingStart はデッキを検証し（card サービス経由）、マッチメイキングサービスに enqueue する。
func (m *Manager) handleMatchmakingStart(ctx context.Context, conn *Connection, data json.RawMessage) {
	var req MatchmakingStartMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sendError(conn, "invalid_data", "invalid matchmaking_start data", false)
		return
	}

	me, err := m.accountClient.GetMe(ctx)
	if err != nil {
		sendError(conn, "matchmaking_error", "failed to fetch player profile: "+err.Error(), true)
		return
	}
	if me.OnboardingStatus != apiaccount.OnboardingStatusCompleted {
		sendError(conn, "matchmaking_error", "onboarding not completed", false)
		return
	}

	if msg, err := m.checkAndIncrementBattleLimit(ctx); err != nil {
		sendError(conn, "matchmaking_error", err.Error(), false)
		return
	} else if msg != "" {
		sendError(conn, "matchmaking_error", msg, false)
		return
	}

	if err := m.cardClient.ValidateDeckForBattle(ctx, req.DeckID); err != nil {
		sendError(conn, "matchmaking_error", "deck validation failed: "+err.Error(), false)
		return
	}

	var name string
	if me.Name != nil {
		name = *me.Name
	}
	if err := m.matchmakingClient.Enqueue(ctx, req.DeckID, name, me.Level); err != nil {
		isRetryable := errors.Is(err, port.ErrMatchmakingUnavailable)
		sendError(conn, "matchmaking_error", "failed to enqueue: "+err.Error(), isRetryable)
		return
	}
	m.startMatchWait(conn.playerID)
	conn.SendMessage(&WSMessage{Type: genws.WSServerMsgMatchmakingStarted})
}

func (m *Manager) handleNpcBattleStart(ctx context.Context, conn *Connection, data json.RawMessage) {
	var req NPCBattleStartMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sendError(conn, "invalid_data", "invalid npc_battle_start data", false)
		return
	}

	me, err := m.accountClient.GetMe(ctx)
	if err != nil {
		sendError(conn, "npc_battle_error", "failed to fetch player profile: "+err.Error(), true)
		return
	}
	if me.OnboardingStatus != apiaccount.OnboardingStatusCompleted {
		sendError(conn, "npc_battle_error", "onboarding not completed", false)
		return
	}

	if msg, err := m.checkAndIncrementBattleLimit(ctx); err != nil {
		sendError(conn, "npc_battle_error", err.Error(), false)
		return
	} else if msg != "" {
		sendError(conn, "npc_battle_error", msg, false)
		return
	}

	if err := m.cardClient.ValidateDeckForBattle(ctx, req.DeckID); err != nil {
		sendError(conn, "npc_battle_error", "deck validation failed: "+err.Error(), false)
		return
	}

	cards, deckInitiatives, err := m.resolveDeckCards(ctx, conn.playerID, req.DeckID)
	if err != nil {
		sendError(conn, "npc_battle_error", "failed to resolve deck", true)
		return
	}

	npcDisplayName, err := m.resolveNpcDisplayName(ctx, req.NpcModel)
	if err != nil {
		sendError(conn, "npc_battle_error", "failed to resolve npc model: "+err.Error(), true)
		return
	}

	var meName string
	if me.Name != nil {
		meName = *me.Name
	}
	player1Summary := service.PlayerSummaryRequest{Name: meName, Level: &me.Level}
	player2Summary := service.PlayerSummaryRequest{Name: npcDisplayName}

	game, err := m.battleClient.StartNPCBattle(ctx, cards, deckInitiatives, req.NpcModel, player1Summary, player2Summary)
	if err != nil {
		sendError(conn, "npc_battle_error", err.Error(), true)
		return
	}
	if m.gamePlayerRepo != nil {
		if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 1, conn.playerID); err != nil {
			slog.Error("npc battle insert game_player failed", "error", err)
		}
	}
	conn.SendMessage(&WSMessage{
		Type: genws.WSServerMsgNpcBattleCreated,
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
func (m *Manager) HandleMatchMade(ctx context.Context, event apimatchmaking.MatchMadeEvent) error {
	if len(event.Players) != 2 {
		return errors.New("match_made event must contain exactly 2 players")
	}

	// どの Pod が接続を保持するかに関わらず待機タイマーを停止（保持していない Pod では noop）
	m.stopMatchWait(event.Players[0].PlayerID)
	m.stopMatchWait(event.Players[1].PlayerID)

	p1Cards, p1Initiatives, err := m.resolveDeckCards(ctx, event.Players[0].PlayerID, event.Players[0].DeckID)
	if err != nil {
		return err
	}
	p2Cards, p2Initiatives, err := m.resolveDeckCards(ctx, event.Players[1].PlayerID, event.Players[1].DeckID)
	if err != nil {
		return err
	}

	p1Summary := matchedPlayerToSummary(event.Players[0])
	p2Summary := matchedPlayerToSummary(event.Players[1])

	game, err := m.battleClient.CreatePvPGame(ctx, p1Cards, p2Cards, p1Initiatives, p2Initiatives, p1Summary, p2Summary)
	if err != nil {
		return err
	}

	if m.gamePlayerRepo != nil {
		if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 1, event.Players[0].PlayerID); err != nil {
			slog.Error("match_made insert game_player p1 failed", "error", err)
		}
		if err := m.gamePlayerRepo.InsertGamePlayer(ctx, game.GameID, 2, event.Players[1].PlayerID); err != nil {
			slog.Error("match_made insert game_player p2 failed", "error", err)
		}
	}

	m.Relay.NotifyMatchFound(game.GameID, event.Players[0].PlayerID, event.Players[1].PlayerID)
	return nil
}

func (m *Manager) resolveNpcDisplayName(ctx context.Context, npcModel string) (string, error) {
	models, err := m.battleClient.ListNpcModels(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range models {
		if e.Model == npcModel {
			return e.DisplayName, nil
		}
	}
	return "", fmt.Errorf("npc model not found: %s", npcModel)
}

func matchedPlayerToSummary(p apimatchmaking.MatchedPlayer) service.PlayerSummaryRequest {
	level := p.Level
	return service.PlayerSummaryRequest{Name: p.Name, Level: &level}
}

// resolveDeckCards はデッキを battle 転送用の情報に展開する。
func (m *Manager) resolveDeckCards(ctx context.Context, playerID string, deckID int64) ([]service.BattleDeckCard, service.DeckInitiatives, error) {
	deckCards, deckInitiatives, err := m.cardClient.GetDeckCards(ctx, deckID)
	if err != nil {
		return nil, service.DeckInitiatives{}, err
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
	initiatives := service.DeckInitiatives{
		RoutineID: deckInitiatives.RoutineID,
		SpecialID: deckInitiatives.SpecialID,
	}
	return cards, initiatives, nil
}

func (m *Manager) checkAndIncrementBattleLimit(ctx context.Context) (string, error) {
	if m.accountClient == nil {
		return "", nil
	}
	limitResp, err := m.accountClient.GetBattleLimit(ctx)
	if err != nil {
		return "", err
	}
	if !limitResp.CanBattle {
		return "daily battle limit reached", nil
	}
	if err := m.accountClient.IncrementBattleCount(ctx); err != nil {
		return "", err
	}
	return "", nil
}
