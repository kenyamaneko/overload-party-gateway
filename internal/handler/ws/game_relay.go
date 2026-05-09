package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	gamedesign "github.com/kenyamaneko/overload-party-common/packages/game-design-constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
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

// PlayerLookupFunc はプレイヤー ID から表示名とレベルを解決する関数型です
type PlayerLookupFunc func(ctx context.Context, playerID string) (name string, level int64, err error)

// 下流呼び出しの既定タイムアウト。WS コネクション ctx を親に持たせた上で
// 個別呼び出しの上限として使用する。
const downstreamCallTimeout = 10 * time.Second

// GameRelay はゲームメンバーシップを管理し、プレイヤーと battle server 間のアクション/状態を中継します。
// 依存は全て NewGameRelay で注入する。accountClient / gamePlayerRepo / playerLookup は nil 許容で、
// nil の場合は EXP 付与や表示名解決をスキップする（ローカル開発 / テスト向け）。
type GameRelay struct {
	hub            *ConnectionHub
	battleClient   service.BattleClient
	spectateRelay  *SpectateRelay
	playerLookup   PlayerLookupFunc
	accountClient  *accountclient.Client
	gamePlayerRepo port.GamePlayerRepo

	mu          sync.RWMutex
	gameMembers map[string][]string      // gameID → []playerID
	playerGames map[string]playerSession // playerID → session (gameID + playerNum)

	timerMu    sync.Mutex
	turnTimers map[string]*turnTimerInfo // gameID → active turn timer
}

// NewGameRelay は GameRelay を生成します。
// accountClient / gamePlayerRepo / playerLookup は nil 可（mock モード / テスト用）。
func NewGameRelay(
	hub *ConnectionHub,
	battleClient service.BattleClient,
	spectateRelay *SpectateRelay,
	accountClient *accountclient.Client,
	gamePlayerRepo port.GamePlayerRepo,
	playerLookup PlayerLookupFunc,
) *GameRelay {
	return &GameRelay{
		hub:            hub,
		battleClient:   battleClient,
		spectateRelay:  spectateRelay,
		accountClient:  accountClient,
		gamePlayerRepo: gamePlayerRepo,
		playerLookup:   playerLookup,
		gameMembers:    make(map[string][]string),
		playerGames:    make(map[string]playerSession),
		turnTimers:     make(map[string]*turnTimerInfo),
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
		log.Printf("ERROR: marshal broadcast message for game %s (type=%s): %v", gameID, msg.Type, err)
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
	var spectateState json.RawMessage // 観戦者に送る正規オブザーバービュー

	// SendGameStateToPlayers はタイマー発火・game_over 後の整合等、特定の WS リクエストに
	// 紐づかない経路からも呼ばれるため、独立したタイムアウトを使う。
	ctx, cancel := context.WithTimeout(context.Background(), downstreamCallTimeout)
	defer cancel()
	for i, pid := range players {
		pNum := r.resolvePlayerNum(pid)
		if pNum == 0 {
			continue
		}
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pNum)
		if err != nil {
			log.Printf("ERROR: get game state for player %s in game %s: %v", pid, gameID, err)
			sendErrorToPlayer(r.hub, pid, "game_state_error", "failed to retrieve game state", true)
			continue
		}

		var meta battleStateMeta
		if err := json.Unmarshal(state, &meta); err != nil {
			// battle server からの state が想定外の構造。ターンタイマー更新は諦めるが
			// クライアントへの状態転送自体は継続する（将来追加されたフィールドへの前方互換）。
			log.Printf("ERROR: extract turn timer for player %s in game %s: %v", pid, gameID, err)
		} else if meta.IsMyTurn {
			activePlayerID = pid
			activeTimeBank = meta.MyView.TimeBank
		}

		r.hub.SendToPlayer(pid, &WSMessage{
			Type: genws.WSServerMsgGameState,
			Data: state,
		})

		if i == 0 {
			spectateState = state
		}
	}

	if activePlayerID != "" {
		r.resetTurnTimer(gameID, activePlayerID, activeTimeBank)
	}

	if r.spectateRelay != nil && spectateState != nil {
		r.spectateRelay.BroadcastStateUpdate(gameID, spectateState)
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
			log.Printf("ERROR: get turn controls for player %s in game %s: %v", pid, gameID, err)
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
	for _, evt := range result.Events {
		switch {
		case evt.EventType == gamelogic.EventTypeTurnStart:
			r.sendActionToPlayers(ctx, gameID, players, evt)

		case evt.PlayerNum != nil && *evt.PlayerNum == int64(actingPlayerNum):
			opponents := make([]string, 0, len(players))
			for _, pid := range players {
				if pid != actingPlayerID {
					opponents = append(opponents, pid)
				}
			}
			r.sendActionToPlayers(ctx, gameID, opponents, evt)

		default:
			if len(evt.State) == 0 {
				continue
			}
			r.hub.SendToPlayer(actingPlayerID, &WSMessage{
				Type: genws.WSServerMsgActionPerformed,
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

// maxNpcTurnIterations は runNpcTurns の上限イテレーション数。
// battle server が NpcPending をクリアしないバグへの安全弁。
const maxNpcTurnIterations = 200

// runNpcTurns は battle server が NpcPending=true を返す間 AdvanceNpcTurn を繰り返し呼び出す。
// 各イテレーションのイベントは sendActionPerformed で中継され、プレイヤーは全 NPC アクションを順に受け取る。
// battle server が NpcPending をクリアしないバグへの安全弁として maxNpcTurnIterations で上限を設ける。
func (r *GameRelay) runNpcTurns(ctx context.Context, gameID, playerID string, current *service.ActionResult) *service.ActionResult {
	for i := 0; current != nil && current.NpcPending && !current.GameOver; i++ {
		if i >= maxNpcTurnIterations {
			log.Printf("ERROR: runNpcTurns iteration cap reached (game=%s, player=%s) — possible battle server bug", gameID, playerID)
			return current
		}
		next, err := r.battleClient.AdvanceNpcTurn(ctx, gameID)
		if err != nil {
			if isCanceled(err) {
				log.Printf("runNpcTurns canceled (game=%s, player=%s): %v", gameID, playerID, err)
			} else {
				log.Printf("ERROR: advance NPC turn loop (game=%s, player=%s): %v", gameID, playerID, err)
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
				log.Printf("get game state for action_performed canceled (player %s): %v", pid, err)
				continue
			}
			log.Printf("ERROR: get game state for action_performed (player %s, game %s): %v", pid, gameID, err)
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

	if r.spectateRelay != nil {
		r.spectateRelay.UnregisterGame(gameID, winningPlayerNum, reason)
	}
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
		log.Printf("ERROR: lookup player_num for %s in game %s: %v", conn.playerID, req.GameID, err)
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
		log.Printf("ERROR: advance NPC turn (game %s): %v", gameID, err)
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
		log.Printf("ERROR: cannot resolve player_num for %s in game %s", conn.playerID, gameID)
		sendError(conn, "game_error", "player not in game", false)
		return
	}
	rawState, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pNum)
	if err != nil {
		if isCanceled(err) {
			return
		}
		log.Printf("ERROR: get game state for battle_start (player %s): %v", conn.playerID, err)
		sendErrorToPlayer(r.hub, conn.playerID, "game_state_error", "failed to retrieve game state", true)
		return
	}

	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		if isCanceled(err) {
			return
		}
		log.Printf("ERROR: lookup game players for battle_start (game %s): %v", gameID, err)
		sendErrorToPlayer(r.hub, conn.playerID, "game_state_error", "failed to retrieve game metadata", true)
		return
	}
	matchType := gamedesign.MatchTypePvp
	if len(entries) == 1 {
		matchType = gamedesign.MatchTypeNpc
	}

	battleStartData := map[string]interface{}{
		"match_type": matchType,
	}

	myName, myLevel := r.lookupPlayer(ctx, conn.playerID)
	var oppName string
	var oppLevel int64
	if matchType == gamedesign.MatchTypeNpc {
		oppName, oppLevel = "NPC", 0
	} else {
		opponentID := r.findOpponent(entries, conn.playerID)
		oppName, oppLevel = r.lookupPlayer(ctx, opponentID)
	}

	battleStartData["my_name"] = myName
	battleStartData["my_level"] = myLevel
	battleStartData["opponent_name"] = oppName
	battleStartData["opponent_level"] = oppLevel

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
		log.Printf("ERROR: parse state meta for turn_start (game %s): %v", gameID, err)
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

// lookupPlayer はプレイヤーの表示名とレベルを解決する。
// 表示用メタデータの解決失敗は battle 進行をブロックしないので、エラー時は空値で続行する
// （クライアントは "" / 0 をプレースホルダとして表示）。
func (r *GameRelay) lookupPlayer(ctx context.Context, playerID string) (string, int64) {
	if r.playerLookup == nil {
		return "", 0
	}
	name, level, err := r.playerLookup(ctx, playerID)
	if err != nil {
		log.Printf("lookup player %s (continuing with empty profile): %v", playerID, err)
		return "", 0
	}
	return name, level
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
// game over 後に gameMembers / playerGames をクリーンアップする。
func (r *GameRelay) leaveAllPlayers(gameID string) {
	r.mu.RLock()
	players := make([]string, len(r.gameMembers[gameID]))
	copy(players, r.gameMembers[gameID])
	r.mu.RUnlock()
	for _, pid := range players {
		r.LeaveGame(pid)
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
func (r *GameRelay) HandleDisconnectTimeout(playerID, gameID string) {
	r.cancelTurnTimer(gameID)

	pNum := r.resolvePlayerNum(playerID)
	if pNum == 0 {
		return
	}
	// 切断タイムアウトは WS コネクション喪失後に発火するので Background ベースで実行する。
	ctx, cancel := context.WithTimeout(context.Background(), downstreamCallTimeout)
	defer cancel()
	result, err := r.battleClient.ProcessAction(ctx, gameID, pNum, gamelogic.ActionTypeForfeit, forfeitReason(gamelogic.WinReasonDisconnect))
	if err != nil {
		// 切断 forfeit は対戦相手にも影響する（ゲーム終了せず宙ぶらりんになる）。
		// 接続は既に切れているので本人通知は不可能だが、対戦相手は DB 観測 / 別経路の
		// 再接続でリカバリする想定。要監視。
		log.Printf("ERROR: disconnect forfeit (game=%s, player=%s, opponent stuck risk): %v", gameID, playerID, err)
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
		log.Printf("ERROR: lookup match type for game %s: %v", gameID, err)
		return ""
	}
	if len(entries) == 1 {
		return gamedesign.MatchTypeNpc
	}
	return gamedesign.MatchTypePvp
}

// findOpponent は game_players エントリから対戦相手の playerID を返す。
func (r *GameRelay) findOpponent(entries []port.GamePlayerEntry, selfID string) string {
	for _, e := range entries {
		if e.PlayerID != selfID {
			return e.PlayerID
		}
	}
	return ""
}

func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// forfeitReason は forfeit リクエスト用のアクションデータを構築する。
func forfeitReason(reason string) json.RawMessage {
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
