package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// spectatorInfo は観戦者の接続と参加時刻を保持する。
type spectatorInfo struct {
	conn     *Connection
	joinedAt time.Time
}

// SpectateRelay はアクティブなゲームの観戦接続を管理します。
// 観戦者がゲームメンバーシップや切断/forfeit ロジックに影響しないよう
// GameRelay とは意図的に分離している。
type SpectateRelay struct {
	hub            *ConnectionHub
	battleClient   service.BattleClient
	gamePlayerRepo port.GamePlayerRepo
	resolver       port.DisplayResolver // spectate_joined バナーデータ用。nil 可

	mu          sync.RWMutex
	spectators  map[string]map[string]*spectatorInfo // gameID → spectatorID → spectatorInfo
	activeGames map[string]time.Time                 // gameID → startedAt
}

// NewSpectateRelay は SpectateRelay を生成します。
// gamePlayerRepo / resolver は nil 可（mock モード / テスト用）。
func NewSpectateRelay(
	hub *ConnectionHub,
	battleClient service.BattleClient,
	gamePlayerRepo port.GamePlayerRepo,
	resolver port.DisplayResolver,
) *SpectateRelay {
	return &SpectateRelay{
		hub:            hub,
		battleClient:   battleClient,
		gamePlayerRepo: gamePlayerRepo,
		resolver:       resolver,
		spectators:     make(map[string]map[string]*spectatorInfo),
		activeGames:    make(map[string]time.Time),
	}
}

// RegisterGame はゲームをアクティブかつ観戦可能として登録します
func (sr *SpectateRelay) RegisterGame(gameID string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.activeGames[gameID] = time.Now()
}

// UnregisterGame は終了したゲームの観戦状態をクリーンアップし、全観戦者に spectate_ended を送信します
func (sr *SpectateRelay) UnregisterGame(gameID string, winningPlayerNum int64, winReason string) {
	sr.mu.Lock()
	spectatorMap := sr.spectators[gameID]
	delete(sr.spectators, gameID)
	delete(sr.activeGames, gameID)
	sr.mu.Unlock()

	if len(spectatorMap) == 0 {
		return
	}

	msg := &WSMessage{
		Type: genws.WSServerMsgSpectateEnded,
		Data: mustMarshal(SpectateEndedMessage{
			GameID:           gameID,
			WinningPlayerNum: winningPlayerNum,
			WinReason:        winReason,
		}),
	}
	for _, info := range spectatorMap {
		info.conn.SendMessage(msg)
	}
}

// HandleSpectateJoin は spectate_join メッセージを処理します。
// battle server 経由でゲームの存在を確認し、観戦者を追加して現在のゲーム状態を返す。
func (sr *SpectateRelay) HandleSpectateJoin(conn *Connection, data json.RawMessage) {
	var req SpectateJoinMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sr.sendSpectateError(conn, "invalid_data", "invalid spectate_join data")
		return
	}

	sr.mu.RLock()
	_, knownGame := sr.activeGames[req.GameID]
	sr.mu.RUnlock()

	if !knownGame {
		sr.sendSpectateError(conn, "game_not_found", "game not found or not active")
		return
	}

	// player1 (num=1) のゲーム状態を正規のオブザーバービューとして取得。
	// WS リクエスト経路のため conn.Context() を親に使い、切断時に下流呼び出しをキャンセルする。
	ctx, cancel := context.WithTimeout(conn.Context(), 10*time.Second)
	defer cancel()
	rawState, err := sr.battleClient.GetGameStateForPlayer(ctx, req.GameID, 1)
	if err != nil || rawState == nil {
		sr.sendSpectateError(conn, "state_unavailable", "could not retrieve game state")
		return
	}

	// プレイヤー名とレベルを resolver 経由で解決 (cache hit が期待値、miss 時は account フォールバック)
	var p1Name, p2Name string
	var p1Level, p2Level int64
	if sr.resolver != nil && sr.gamePlayerRepo != nil {
		entries, err := sr.gamePlayerRepo.LookupGamePlayers(ctx, req.GameID)
		if err != nil {
			slog.Error("spectate: lookup game players failed", "game_id", req.GameID, "error", err)
		} else {
			for _, e := range entries {
				meta := sr.resolver.Resolve(ctx, req.GameID, e.PlayerNum, e.PlayerID)
				switch e.PlayerNum {
				case 1:
					p1Name, p1Level = meta.Name, int64(meta.Level)
				case 2:
					p2Name, p2Level = meta.Name, int64(meta.Level)
				}
			}
			if len(entries) == 1 {
				p2Name = "NPC"
			}
		}
	}

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
		Type: genws.WSServerMsgSpectateJoined,
		Data: mustMarshal(SpectateJoinedMessage{
			GameID:       req.GameID,
			Player1Name:  p1Name,
			Player1Level: p1Level,
			Player2Name:  p2Name,
			Player2Level: p2Level,
			State:        rawState,
		}),
	})

	slog.Info("spectator joined", "player_id", conn.playerID, "game_id", req.GameID)
}

// HandleSpectateLeave は spectate_leave メッセージを処理します
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

	slog.Info("spectator left", "player_id", conn.playerID, "game_id", req.GameID)
}

// RemoveSpectator は観戦中の全ゲームから観戦者を除去します。
// WebSocket 接続クローズ時に呼ばれる。
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

// HandleSpectateStamp は spectate_stamp メッセージを処理しブロードキャストします
func (sr *SpectateRelay) HandleSpectateStamp(conn *Connection, data json.RawMessage) {
	var req SpectateStampMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}

	msg := &WSMessage{
		Type: genws.WSServerMsgSpectateStampBroadcast,
		Data: mustMarshal(SpectateStampBroadcastMessage{
			GameID:      req.GameID,
			SpectatorID: conn.playerID,
			StampNo:     req.StampNo,
		}),
	}

	sr.broadcastToSpectators(req.GameID, msg)
}

// BroadcastStateUpdate はゲームの全観戦者に spectate_update メッセージを送信します。
// ゲーム状態が変化するたび（game_state 送信後）に呼ばれる。
func (sr *SpectateRelay) BroadcastStateUpdate(gameID string, state json.RawMessage) {
	msg := &WSMessage{
		Type: genws.WSServerMsgSpectateUpdate,
		Data: state,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("spectate: marshal update failed", "error", err)
		return
	}

	sr.mu.RLock()
	defer sr.mu.RUnlock()
	for _, info := range sr.spectators[gameID] {
		info.conn.SendRaw(data)
	}
}

// IsSpectator は指定プレイヤーがいずれかのゲームを観戦中かどうかを返します
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
		slog.Error("spectate: marshal broadcast failed", "error", err)
		return
	}

	sr.mu.RLock()
	defer sr.mu.RUnlock()
	for _, info := range sr.spectators[gameID] {
		info.conn.SendRaw(data)
	}
}

// ActiveGames は現在アクティブなゲーム一覧をプレイヤー情報付きで返します。
// REST リクエスト経由で呼ばれるので、クライアント切断で DB 検索をキャンセルできるように親 ctx を受け取る。
func (sr *SpectateRelay) ActiveGames(parent context.Context) []apigateway.SpectateGameInfo {
	sr.mu.RLock()
	gameIDs := make(map[string]time.Time, len(sr.activeGames))
	for gid, t := range sr.activeGames {
		gameIDs[gid] = t
	}
	sr.mu.RUnlock()

	if len(gameIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	result := make([]apigateway.SpectateGameInfo, 0, len(gameIDs))
	for gameID, startedAt := range gameIDs {
		info := apigateway.SpectateGameInfo{
			GameID:    gameID,
			StartedAt: startedAt,
		}
		if sr.gamePlayerRepo != nil {
			entries, err := sr.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
			if err != nil {
				slog.Error("spectate: lookup players for active game failed", "game_id", gameID, "error", err)
			} else {
				for _, e := range entries {
					switch e.PlayerNum {
					case 1:
						info.Player1ID = e.PlayerID
					case 2:
						info.Player2ID = e.PlayerID
					}
				}
			}
		}
		result = append(result, info)
	}
	return result
}

func (sr *SpectateRelay) sendSpectateError(conn *Connection, code, message string) {
	conn.SendMessage(&WSMessage{
		Type: genws.WSServerMsgSpectateError,
		Data: mustMarshal(SpectateErrorMessage{ErrorCode: code, Message: message}),
	})
}
