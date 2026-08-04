package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	gamedesign "github.com/kenyamaneko/overload-party-common/packages/game-design-constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// battleStateMeta は battle server のゲーム状態 JSON の最小射影。
// ターン関連メタデータ（タイマー、アクティブプレイヤー）の抽出にのみ使用する。
// gateway はゲーム状態を変換せずそのままパススルーする。
type battleStateMeta struct {
	CurrentTurn int64 `json:"currentTurn"`
	IsMyTurn    bool  `json:"isMyTurn"`
	MyView      struct {
		TimeBank int64 `json:"timeBank"`
	} `json:"myView"`
}

// playerSession は game_enter 時に DB からキャッシュされるプレイヤーのゲーム内状態。
type playerSession struct {
	gameID    string
	playerNum int
}

// 下流呼び出しの既定タイムアウト。WS コネクション ctx を親に持たせた上で
// 個別呼び出しの上限として使用する。
const downstreamCallTimeout = 10 * time.Second

// GameRelay はゲームメンバーシップを管理し、プレイヤーと battle server 間のアクション/状態を中継します。
type GameRelay struct {
	hub            *ConnectionHub
	battleClient   service.BattleClient
	accountClient  port.AccountClient
	gamePlayerRepo port.GamePlayerRepo

	mu          sync.RWMutex
	gameMembers map[string][]string      // gameID → []playerID
	playerGames map[string]playerSession // playerID → session (gameID + playerNum)

	timerMu    sync.Mutex
	turnTimers map[string]*turnTimerInfo // gameID → active turn timer

	// timerStore はターン期限を Redis へ写す。未設定 (nil) の場合は写しを行わない
	// （ローカル開発など Redis を使わない環境向け）。
	timerStore port.TimerStore
}

// NewGameRelay は GameRelay を生成します。
// accountClient / gamePlayerRepo / timerStore は nil 可（mock モード / テスト用 / 写し無し）。
func NewGameRelay(
	hub *ConnectionHub,
	battleClient service.BattleClient,
	accountClient port.AccountClient,
	gamePlayerRepo port.GamePlayerRepo,
	timerStore port.TimerStore,
) *GameRelay {
	return &GameRelay{
		hub:            hub,
		battleClient:   battleClient,
		accountClient:  accountClient,
		gamePlayerRepo: gamePlayerRepo,
		gameMembers:    make(map[string][]string),
		playerGames:    make(map[string]playerSession),
		turnTimers:     make(map[string]*turnTimerInfo),
		timerStore:     timerStore,
	}
}

// GameIDForPlayer はプレイヤーの gameID を返します
func (r *GameRelay) GameIDForPlayer(playerID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, ok := r.playerGames[playerID]
	if !ok {
		return "", false
	}
	return sess.gameID, true
}

// NotifyOpponentDisconnected はプレイヤー切断時に対戦相手に opponent_disconnected を送信します
func (r *GameRelay) NotifyOpponentDisconnected(playerID, gameID string) {
	r.sendToOpponent(playerID, gameID, genws.WSServerMsgOpponentDisconnected)
}

// NotifyOpponentReconnected は切断したプレイヤーの復帰時に対戦相手に opponent_reconnected を送信します
func (r *GameRelay) NotifyOpponentReconnected(playerID, gameID string) {
	r.sendToOpponent(playerID, gameID, genws.WSServerMsgOpponentReconnected)
}

// HandleReconnect はプレイヤーが切断していたゲームへ復帰した際の処理です。
// wasLate は復帰したプレイヤー自身の猶予期限が既に過ぎていたかを表します。
//
// resolveStaleDisconnect は WS ハンドシェイク中に同期実行すると pump 起動を遅らせるため、別 goroutine で実行する。
func (r *GameRelay) HandleReconnect(playerID, gameID string, wasLate bool) {
	r.NotifyOpponentReconnected(playerID, gameID)
	go r.resolveStaleDisconnect(gameID, playerID, wasLate)
}

// resolveStaleDisconnect は全員切断のまま決着していなかった対戦を、戻ってきた
// プレイヤーを起点に評価します。対戦相手が接続中、または対戦相手の猶予が
// まだ残っている場合は何もしません (対戦相手不在の対戦継続、または通常の
// 復帰として扱う)。
func (r *GameRelay) resolveStaleDisconnect(gameID, returningPlayerID string, returningWasLate bool) {
	if r.gamePlayerRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), downstreamCallTimeout)
	defer cancel()

	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		slog.Error("resolve stale disconnect: lookup game players failed", "game_id", gameID, "player_id", returningPlayerID, "error", err)
		return
	}
	opponentID := opponentPlayerID(entries, returningPlayerID)
	if opponentID == "" || r.hub.IsConnected(opponentID) {
		return
	}
	opponentExpired, err := r.hub.IsDisconnectDeadlineExpired(opponentID)
	if err != nil {
		slog.Error("resolve stale disconnect: check opponent disconnect deadline failed, leaving unresolved", "opponent_id", opponentID, "game_id", gameID, "error", err)
		return
	}
	if !opponentExpired {
		return
	}

	if !returningWasLate {
		// 対戦相手だけ猶予切れ → 復帰した側の勝ち。既存の forfeit 経路を対戦相手の
		// playerID で呼び出すことで、対戦相手の forfeit として処理する。
		r.HandleDisconnectTimeout(opponentID, gameID)
		return
	}

	// 両者強制決着は勝者をプレイヤー番号から決めないため、復帰した側の番号を渡す。
	pNum := playerNumOf(entries, returningPlayerID)
	if pNum == 0 {
		slog.Error("resolve stale disconnect: returning player has no slot in the game, leaving unresolved", "game_id", gameID, "returning_player_id", returningPlayerID)
		return
	}

	r.cancelTurnTimer(gameID)
	result, err := r.battleClient.ProcessAction(ctx, gameID, pNum, gamelogic.ActionTypeForfeitBoth, buildForfeitReason(gamelogic.WinReasonDisconnect))
	if err != nil {
		// 未決着のまま残った対戦は次に誰かが戻るまで解消しないため、要監視。
		slog.Error("both-side forfeit failed, game left unresolved", "game_id", gameID, "opponent_id", opponentID, "returning_player_id", returningPlayerID, "error", err)
		return
	}
	if result != nil && result.GameOver {
		r.broadcastGameOver(gameID, result.WinningPlayerNum, result.WinReason)
		r.leaveAllPlayers(gameID)
	}
}

// areBothPlayersDisconnected は gameID の 2 人対戦で playerID と対戦相手の双方が、
// 現在 WS 接続を持たないかを判定する。gamePlayerRepo が無い場合や対戦相手が
// 解決できない場合 (NPC 戦など) は false を返す。
func (r *GameRelay) areBothPlayersDisconnected(ctx context.Context, playerID, gameID string) (bool, error) {
	if r.gamePlayerRepo == nil {
		return false, nil
	}
	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		return false, err
	}
	opponentID := opponentPlayerID(entries, playerID)
	if opponentID == "" {
		return false, nil
	}
	return !r.hub.IsConnected(playerID) && !r.hub.IsConnected(opponentID), nil
}

// opponentPlayerID は 2 人制のゲームで selfID の対戦相手の playerID を返す。
// 該当が無い場合 (NPC 戦や不整合データ) は空文字を返す。
func opponentPlayerID(entries []port.GamePlayerEntry, selfID string) string {
	if len(entries) != 2 {
		return ""
	}
	for _, e := range entries {
		if e.PlayerID != selfID {
			return e.PlayerID
		}
	}
	return ""
}

// playerNumOf は entries から playerID のプレイヤー番号を返す。
// 該当が無い場合は 0 を返す。
func playerNumOf(entries []port.GamePlayerEntry, playerID string) int {
	for _, e := range entries {
		if e.PlayerID == playerID {
			return e.PlayerNum
		}
	}
	return 0
}

func (r *GameRelay) sendToOpponent(playerID, gameID, msgType string) {
	r.mu.RLock()
	members := r.gameMembers[gameID]
	r.mu.RUnlock()

	for _, pid := range members {
		if pid != playerID {
			r.hub.SendToPlayer(pid, &WSMessage{Type: msgType})
			return
		}
	}
}

// JoinGame はプレイヤーをゲームに参加させます
func (r *GameRelay) JoinGame(playerID, gameID string, playerNum int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if oldSess, ok := r.playerGames[playerID]; ok && oldSess.gameID != gameID {
		r.gameMembers[oldSess.gameID] = removeString(r.gameMembers[oldSess.gameID], playerID)
		if len(r.gameMembers[oldSess.gameID]) == 0 {
			delete(r.gameMembers, oldSess.gameID)
		}
	}

	r.playerGames[playerID] = playerSession{gameID: gameID, playerNum: playerNum}
	r.gameMembers[gameID] = appendUnique(r.gameMembers[gameID], playerID)
}

// LeaveGame はプレイヤーをゲームから離脱させます
func (r *GameRelay) LeaveGame(playerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sess, ok := r.playerGames[playerID]; ok {
		delete(r.playerGames, playerID)
		r.gameMembers[sess.gameID] = removeString(r.gameMembers[sess.gameID], playerID)
		if len(r.gameMembers[sess.gameID]) == 0 {
			delete(r.gameMembers, sess.gameID)
		}
	}
}

// BroadcastToGame はゲーム内の全プレイヤーに同一メッセージを送信します
func (r *GameRelay) BroadcastToGame(gameID string, msg *WSMessage) {
	// mustMarshal は内部構造体専用で panic する。BroadcastToGame は battle server 由来の
	// json.RawMessage を Data に格納したメッセージ（例: game_state）も流すため、ここでは
	// json.Marshal を直接使う。エンベロープに json.RawMessage を入れる場合は失敗しないはずだが、
	// 念のためエラーログを出して継続する（broadcast 自体を panic させない）。
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("marshal broadcast message failed", "game_id", gameID, "type", msg.Type, "error", err)
		return
	}

	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	for _, pid := range players {
		r.hub.SendRawToPlayer(pid, data)
	}
}

// SendGameStateToPlayers はゲーム内の全プレイヤーにゲーム状態を送信します
func (r *GameRelay) SendGameStateToPlayers(gameID string) {
	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	var activePlayerID string
	var activeTimeBank int64

	// SendGameStateToPlayers はタイマー発火・game_over 後の整合等、特定の WS リクエストに
	// 紐づかない経路からも呼ばれるため、独立したタイムアウトを使う。
	ctx, cancel := context.WithTimeout(context.Background(), downstreamCallTimeout)
	defer cancel()
	for _, pid := range players {
		pNum := r.resolvePlayerNum(pid)
		if pNum == 0 {
			continue
		}
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pNum)
		if err != nil {
			slog.Error("get game state for player failed", "player_id", pid, "game_id", gameID, "error", err)
			sendErrorToPlayer(r.hub, pid, "game_state_error", "failed to retrieve game state", true)
			continue
		}

		var meta battleStateMeta
		if err := json.Unmarshal(state, &meta); err != nil {
			// battle server からの state が想定外の構造。ターンタイマー更新は諦めるが
			// クライアントへの状態転送自体は継続する（将来追加されたフィールドへの前方互換）。
			slog.Warn("extract turn timer failed", "player_id", pid, "game_id", gameID, "error", err)
		} else if meta.IsMyTurn {
			activePlayerID = pid
			activeTimeBank = meta.MyView.TimeBank
		}

		r.hub.SendToPlayer(pid, &WSMessage{
			Type: genws.WSServerMsgGameState,
			Data: state,
		})
	}

	if activePlayerID != "" {
		r.resetTurnTimer(gameID, activePlayerID, activeTimeBank)
	}
}

// SendTurnControlsToPlayers はゲーム内の全プレイヤーにターンコントロール情報を送信します。
// SendGameStateToPlayers と同じく WS リクエストに紐づかない経路（タイマー発火等）からも呼ばれるため、独立したタイムアウトを使う。
func (r *GameRelay) SendTurnControlsToPlayers(gameID string) {
	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), downstreamCallTimeout)
	defer cancel()
	for _, pid := range players {
		pNum := r.resolvePlayerNum(pid)
		if pNum == 0 {
			continue
		}
		raw, err := r.battleClient.GetTurnControlsForPlayer(ctx, gameID, pNum)
		if err != nil {
			slog.Error("get turn controls for player failed", "player_id", pid, "game_id", gameID, "error", err)
			sendErrorToPlayer(r.hub, pid, "turn_controls_error", "failed to retrieve turn controls", true)
			continue
		}
		if raw == nil {
			continue
		}
		r.hub.SendToPlayer(pid, &WSMessage{
			Type: genws.WSServerMsgTurnControls,
			Data: raw,
		})
	}
}

// sendActionPerformed はイベントごとに action_performed メッセージをディスパッチする。
//
// ルーティングはイベントメタデータに基づく:
//   - システムイベント (turn_start) → 全プレイヤーに送信
//   - 自分のアクション → 対戦相手に送信（情報隠蔽済み状態を取得）
//   - 他プレイヤーのアクション (NPC) → 行動プレイヤーに送信（battle server の状態付き）
func (r *GameRelay) sendActionPerformed(ctx context.Context, gameID, actingPlayerID string, result *service.ActionResult) {
	if result == nil || len(result.Events) == 0 {
		return
	}

	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	actingPlayerNum := r.resolvePlayerNum(actingPlayerID)
	for _, event := range result.Events {
		switch {
		case event.EventType == gamelogic.EventTypeTurnStart:
			r.sendActionToPlayers(ctx, gameID, players, event)

		case event.PlayerNum != nil && *event.PlayerNum == int64(actingPlayerNum):
			opponents := make([]string, 0, len(players))
			for _, pid := range players {
				if pid != actingPlayerID {
					opponents = append(opponents, pid)
				}
			}
			r.sendActionToPlayers(ctx, gameID, opponents, event)

		default:
			if len(event.State) == 0 {
				continue
			}
			r.hub.SendToPlayer(actingPlayerID, &WSMessage{
				Type: genws.WSServerMsgActionPerformed,
				Data: mustMarshal(ActionPerformedMessage{
					Sequence:   event.Sequence,
					ActionType: event.EventType,
					ActionData: mapToRaw(event.EventData),
					State:      mapToRaw(event.State),
				}),
			})
		}
	}
}

// 原則に従ってパススルーしたいが、battle openapi が event_data / state を
// additionalProperties: true で宣言しており api-battle-rpc-go が map[] を返すため、
// WS 境界で json.RawMessage に変換する。空 map は nil で omitempty 扱い。
func mapToRaw(m map[string]interface{}) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		slog.Warn("marshal action payload failed", "error", err)
		return nil
	}
	return raw
}

// maxNpcTurnIterations は runNpcTurns の上限イテレーション数。
// battle server が NpcPending をクリアしないバグへの安全弁。
const maxNpcTurnIterations = 200

// runNpcTurns は battle server が NpcPending=true を返す間 AdvanceNpcTurn を繰り返し呼び出す。
// 各イテレーションのイベントは sendActionPerformed で中継され、プレイヤーは全 NPC アクションを順に受け取る。
// battle server が NpcPending をクリアしないバグへの安全弁として maxNpcTurnIterations で上限を設ける。
func (r *GameRelay) runNpcTurns(ctx context.Context, gameID, playerID string, current *service.ActionResult) *service.ActionResult {
	for i := 0; current != nil && current.NpcPending && !current.GameOver; i++ {
		if i >= maxNpcTurnIterations {
			slog.Error("runNpcTurns iteration cap reached (possible battle server bug)", "game_id", gameID, "player_id", playerID)
			return current
		}
		next, err := r.battleClient.AdvanceNpcTurn(ctx, gameID)
		if err != nil {
			if isCanceled(err) {
				slog.Info("runNpcTurns canceled", "game_id", gameID, "player_id", playerID, "error", err)
			} else {
				slog.Error("advance NPC turn loop failed", "game_id", gameID, "player_id", playerID, "error", err)
				sendErrorToPlayer(r.hub, playerID, "npc_turn_error", "failed to advance NPC turn", true)
			}
			return current
		}
		r.sendActionPerformed(ctx, gameID, playerID, next)
		current = next
	}
	return current
}

// sendActionToPlayers は各プレイヤーのゲーム状態を取得し action_performed メッセージを送信する。
// 状態は battle server からの変換なしのパススルー。
func (r *GameRelay) sendActionToPlayers(ctx context.Context, gameID string, pids []string, evt service.ActionEvent) {
	for _, pid := range pids {
		pNum := r.resolvePlayerNum(pid)
		if pNum == 0 {
			continue
		}
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pNum)
		if err != nil {
			if isCanceled(err) {
				// 上流（典型的には WS 切断）でキャンセルされた。ループは続行不要、次プレイヤーへ。
				slog.Info("get game state for action_performed canceled", "player_id", pid, "error", err)
				continue
			}
			slog.Error("get game state for action_performed failed", "player_id", pid, "game_id", gameID, "error", err)
			sendErrorToPlayer(r.hub, pid, "game_state_error", "failed to retrieve game state", true)
			continue
		}
		r.hub.SendToPlayer(pid, &WSMessage{
			Type: genws.WSServerMsgActionPerformed,
			Data: mustMarshal(ActionPerformedMessage{
				Sequence:   evt.Sequence,
				ActionType: evt.EventType,
				ActionData: mustMarshal(evt.EventData),
				State:      mustMarshal(state),
			}),
		})
	}
}

func (r *GameRelay) broadcastGameOver(gameID string, winningPlayerNum int64, reason string) {
	r.BroadcastToGame(gameID, &WSMessage{
		Type: genws.WSServerMsgGameOver,
		Data: mustMarshal(GameOverMessage{
			GameID:           gameID,
			WinningPlayerNum: winningPlayerNum,
			WinReason:        reason,
		}),
	})

	r.awardGameExp(gameID, winningPlayerNum, reason)
}

// HandleGameEnter は game_enter メッセージを処理します
func (r *GameRelay) HandleGameEnter(conn *Connection, data json.RawMessage) {
	var req GameEnterMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sendError(conn, "invalid_data", "invalid game_enter data", false)
		return
	}

	ctx, cancel := context.WithTimeout(conn.Context(), downstreamCallTimeout)
	defer cancel()
	playerNum, err := r.gamePlayerRepo.LookupPlayerNum(ctx, req.GameID, conn.playerID)
	if err != nil {
		if isCanceled(err) {
			// 接続切断につき中止
			return
		}
		slog.Error("lookup player_num failed", "player_id", conn.playerID, "game_id", req.GameID, "error", err)
		sendError(conn, "game_error", "player not found in game", false)
		return
	}

	r.JoinGame(conn.playerID, req.GameID, playerNum)
	conn.SendMessage(&WSMessage{
		Type: genws.WSServerMsgGameEntered,
		Data: mustMarshal(map[string]string{"game_id": req.GameID}),
	})

	// battle_start/turn_start はエントリーアニメーション用の初期状態を運ぶ。
	// 続く SendGameStateToPlayers が権威ある状態を配信しターンタイマーを開始する。
	r.sendBattleStartAndTurnStart(conn, req.GameID)
	r.advanceNpcIfNeeded(conn.Context(), req.GameID, conn.playerID)
	r.SendGameStateToPlayers(req.GameID)
	r.SendTurnControlsToPlayers(req.GameID)
}

// advanceNpcIfNeeded は NPC がアクティブプレイヤーの場合に NPC の最初のターンを実行する。
// game_enter 後に NPC アクションイベントが配信されるようにする。
func (r *GameRelay) advanceNpcIfNeeded(parentCtx context.Context, gameID, playerID string) {
	ctx, cancel := context.WithTimeout(parentCtx, downstreamCallTimeout)
	defer cancel()

	matchType := r.lookupMatchType(ctx, gameID)
	if matchType != gamedesign.MatchTypeNpc {
		return
	}

	result, err := r.battleClient.AdvanceNpcTurn(ctx, gameID)
	if err != nil {
		if isCanceled(err) {
			return
		}
		slog.Error("advance NPC turn failed", "game_id", gameID, "error", err)
		sendErrorToPlayer(r.hub, playerID, "npc_turn_error", "failed to advance NPC turn", true)
		return
	}

	r.sendActionPerformed(ctx, gameID, playerID, result)
	result = r.runNpcTurns(ctx, gameID, playerID, result)

	if result != nil && result.GameOver {
		r.cancelTurnTimer(gameID)
		r.broadcastGameOver(gameID, result.WinningPlayerNum, result.WinReason)
		r.leaveAllPlayers(gameID)
	}
}

// sendBattleStartAndTurnStart はゲーム参加プレイヤーに battle_start と turn_start の
// action_performed イベントを送信する。プレイヤープロフィールデータが必要なため
// battle server ではなく gateway が生成する合成イベント。
func (r *GameRelay) sendBattleStartAndTurnStart(conn *Connection, gameID string) {
	ctx, cancel := context.WithTimeout(conn.Context(), downstreamCallTimeout)
	defer cancel()

	pNum := r.resolvePlayerNum(conn.playerID)
	if pNum == 0 {
		slog.Error("cannot resolve player_num", "player_id", conn.playerID, "game_id", gameID)
		sendError(conn, "game_error", "player not in game", false)
		return
	}
	rawState, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pNum)
	if err != nil {
		if isCanceled(err) {
			return
		}
		slog.Error("get game state for battle_start failed", "player_id", conn.playerID, "error", err)
		sendErrorToPlayer(r.hub, conn.playerID, "game_state_error", "failed to retrieve game state", true)
		return
	}

	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		if isCanceled(err) {
			return
		}
		slog.Error("lookup game players for battle_start failed", "game_id", gameID, "error", err)
		sendErrorToPlayer(r.hub, conn.playerID, "game_state_error", "failed to retrieve game metadata", true)
		return
	}
	matchType := gamedesign.MatchTypePvp
	if len(entries) == 1 {
		matchType = gamedesign.MatchTypeNpc
	}

	var clientState apibattle.ClientGameState
	if err := json.Unmarshal(rawState, &clientState); err != nil {
		slog.Error("parse client game state for battle_start failed", "game_id", gameID, "error", err)
		sendErrorToPlayer(r.hub, conn.playerID, "game_state_error", "failed to parse game state", true)
		return
	}
	mySummary, oppSummary := clientState.Player1Summary, clientState.Player2Summary
	if pNum == 2 {
		mySummary, oppSummary = clientState.Player2Summary, clientState.Player1Summary
	}

	// NPC は level を持たないため呼び出し側で 0 に正規化する (legacy client contract で
	// level は number 必須のため null を 0 として渡す)。
	var myLevel, oppLevel int64
	if mySummary.Level != nil {
		myLevel = *mySummary.Level
	}
	if oppSummary.Level != nil {
		oppLevel = *oppSummary.Level
	}

	battleStartData := map[string]interface{}{
		"match_type":     matchType,
		"my_name":        mySummary.Name,
		"my_level":       myLevel,
		"opponent_name":  oppSummary.Name,
		"opponent_level": oppLevel,
	}

	// Sequence 0: battle_start / turn_start は gateway 合成イベントであり battle server のイベントシーケンスに含まれない
	conn.SendMessage(&WSMessage{
		Type: genws.WSServerMsgActionPerformed,
		Data: mustMarshal(ActionPerformedMessage{
			Sequence:   0,
			ActionType: gamelogic.EventTypeBattleStart,
			ActionData: mustMarshal(battleStartData),
			State:      rawState,
		}),
	})

	var stateMeta battleStateMeta
	if err := json.Unmarshal(rawState, &stateMeta); err != nil {
		// turn_start メタが取れなくても battle_start は送信済み。クライアントは続く
		// SendGameStateToPlayers の game_state で同等情報を得るため continue する。
		slog.Warn("parse state meta for turn_start failed", "game_id", gameID, "error", err)
		return
	}
	turnStartData := map[string]interface{}{
		"turn":       stateMeta.CurrentTurn,
		"is_my_turn": stateMeta.IsMyTurn,
	}
	conn.SendMessage(&WSMessage{
		Type: genws.WSServerMsgActionPerformed,
		Data: mustMarshal(ActionPerformedMessage{
			Sequence:   0,
			ActionType: gamelogic.EventTypeTurnStart,
			ActionData: mustMarshal(turnStartData),
			State:      rawState,
		}),
	})
}

// HandleGameAction は game_action メッセージを処理します
func (r *GameRelay) HandleGameAction(ctx context.Context, conn *Connection, data json.RawMessage) {
	var action GameActionMessage
	if err := json.Unmarshal(data, &action); err != nil {
		sendError(conn, "invalid_data", "invalid game_action data", false)
		return
	}

	pNum := r.resolvePlayerNum(conn.playerID)
	if pNum == 0 {
		sendError(conn, "game_error", "player not found in game", false)
		return
	}
	result, err := r.battleClient.ProcessAction(ctx, action.GameID, pNum, action.ActionType, action.Data)
	if err != nil {
		if isCanceled(err) {
			return
		}
		conn.SendMessage(&WSMessage{
			Type: genws.WSServerMsgActionRejected,
			Data: mustMarshal(ActionRejectedMessage{
				GameID:     action.GameID,
				ActionType: action.ActionType,
				Reason:     err.Error(),
			}),
		})
		return
	}

	// action_performed はクライアントアニメーション用のスナップショットを運ぶ。
	// 続く SendGameStateToPlayers が権威ある最終状態を配信しターンタイマーをリセットする。
	r.sendActionPerformed(ctx, action.GameID, conn.playerID, result)
	result = r.runNpcTurns(ctx, action.GameID, conn.playerID, result)
	r.SendGameStateToPlayers(action.GameID)
	r.SendTurnControlsToPlayers(action.GameID)

	if result != nil && result.GameOver {
		r.cancelTurnTimer(action.GameID)
		r.broadcastGameOver(action.GameID, result.WinningPlayerNum, result.WinReason)
		r.leaveAllPlayers(action.GameID)
	}
}

// leaveAllPlayers はゲームの全プレイヤーのメンバーシップを解除する。
// game over 後に gameMembers / playerGames をクリーンアップし、対戦を終えた
// プレイヤーの切断猶予期限の写しも合わせて削除する。
func (r *GameRelay) leaveAllPlayers(gameID string) {
	r.mu.RLock()
	players := make([]string, len(r.gameMembers[gameID]))
	copy(players, r.gameMembers[gameID])
	r.mu.RUnlock()
	for _, pid := range players {
		r.LeaveGame(pid)
		r.hub.ClearDisconnectDeadline(pid)
	}
}

// HandleUseStamp は use_stamp メッセージを処理します
func (r *GameRelay) HandleUseStamp(conn *Connection, data json.RawMessage) {
	var req UseStampMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}
	pNum := r.resolvePlayerNum(conn.playerID)
	if pNum == 0 {
		return
	}
	r.BroadcastToGame(req.GameID, &WSMessage{
		Type: genws.WSServerMsgStampUsed,
		Data: mustMarshal(StampUsedMessage{
			GameID:    req.GameID,
			PlayerNum: int64(pNum),
			StampNo:   req.StampNo,
		}),
	})
}

// HandleDisconnectTimeout は切断タイムアウト後の forfeit を処理します。
// forfeit reason の方針については handleTurnTimeout のコメントを参照。
//
// 対戦相手も切断中の場合はこの時点では決着させない。両者とも不在の対戦は
// その場では勝者を決める理由が無く、次にどちらかが戻ってきた時点で
// resolveStaleDisconnect が改めて評価する。手番の計時もそのため止めない。
func (r *GameRelay) HandleDisconnectTimeout(playerID, gameID string) {
	ctx, cancel := context.WithTimeout(context.Background(), downstreamCallTimeout)
	bothDisconnected, err := r.areBothPlayersDisconnected(ctx, playerID, gameID)
	cancel()
	if err != nil {
		slog.Error("disconnect timeout: lookup game players failed", "game_id", gameID, "player_id", playerID, "error", err)
		return
	}
	if bothDisconnected {
		slog.Info("disconnect grace expired while opponent is also disconnected, deferring resolution", "player_id", playerID, "game_id", gameID)
		return
	}

	r.cancelTurnTimer(gameID)

	pNum := r.resolvePlayerNum(playerID)
	if pNum == 0 {
		return
	}
	// 切断タイムアウトは WS コネクション喪失後に発火するので Background ベースで実行する。
	ctx2, cancel2 := context.WithTimeout(context.Background(), downstreamCallTimeout)
	defer cancel2()
	result, err := r.battleClient.ProcessAction(ctx2, gameID, pNum, gamelogic.ActionTypeForfeit, buildForfeitReason(gamelogic.WinReasonDisconnect))
	if err != nil {
		// 切断 forfeit は対戦相手にも影響する（ゲーム終了せず宙ぶらりんになる）。
		// 接続は既に切れているので本人通知は不可能だが、対戦相手は DB 観測 / 別経路の
		// 再接続でリカバリする想定。要監視。
		slog.Error("disconnect forfeit failed (opponent stuck risk)", "game_id", gameID, "player_id", playerID, "error", err)
		return
	}
	if result != nil && result.GameOver {
		r.broadcastGameOver(gameID, result.WinningPlayerNum, gamelogic.WinReasonDisconnect)
		r.leaveAllPlayers(gameID)
	}
}

// NotifyMatchFound は両プレイヤーに match_found を送信します
func (r *GameRelay) NotifyMatchFound(gameID, player1ID, player2ID string) {
	msg := &WSMessage{
		Type: genws.WSServerMsgMatchFound,
		Data: mustMarshal(MatchFoundMessage{
			GameID: gameID,
		}),
	}
	r.hub.SendToPlayer(player1ID, msg)
	r.hub.SendToPlayer(player2ID, msg)
}

// resolvePlayerNum はインメモリセッションからキャッシュ済み playerNum を返す。
// プレイヤーがゲームに参加していない場合は 0 を返す。
func (r *GameRelay) resolvePlayerNum(playerID string) int {
	r.mu.RLock()
	sess, ok := r.playerGames[playerID]
	r.mu.RUnlock()
	if !ok {
		return 0
	}
	return sess.playerNum
}

// lookupMatchType は game_players の行数からマッチタイプを導出する。
// 失敗時は空文字を返し、呼び出し元は MatchTypeNpc 分岐に入らない（≒ 何もしない）安全側にフォールスルーする。
func (r *GameRelay) lookupMatchType(ctx context.Context, gameID string) string {
	if r.gamePlayerRepo == nil {
		return ""
	}
	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		slog.Error("lookup match type failed", "game_id", gameID, "error", err)
		return ""
	}
	if len(entries) == 1 {
		return gamedesign.MatchTypeNpc
	}
	return gamedesign.MatchTypePvp
}

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// buildForfeitReason は forfeit リクエスト用のアクションデータを構築する。
func buildForfeitReason(reason string) json.RawMessage {
	return mustMarshal(map[string]string{"reason": reason})
}

func removeString(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
