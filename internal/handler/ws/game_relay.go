package ws

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
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

// turnTimerInfo はアクティブなターンタイマーの状態を保持する。
type turnTimerInfo struct {
	timer          *time.Timer
	activePlayerID string
}

// playerSession は game_enter 時に DB からキャッシュされるプレイヤーのゲーム内状態。
type playerSession struct {
	gameID    string
	playerNum int
}

// PlayerLookupFunc はプレイヤー ID から表示名とレベルを解決する関数型です
type PlayerLookupFunc func(ctx context.Context, playerID string) (name string, level int64, err error)

// GameRelay はゲームメンバーシップを管理し、プレイヤーと battle server 間のアクション/状態を中継します
type GameRelay struct {
	hub          *ConnectionHub
	battleClient service.BattleClient

	// 循環依存回避のためコンストラクション後に設定
	spectateRelay  *SpectateRelay
	playerLookup   PlayerLookupFunc
	accountClient  *accountclient.Client
	gamePlayerRepo port.GamePlayerRepo

	mu          sync.RWMutex
	gameMembers map[string][]string          // gameID → []playerID
	playerGames map[string]playerSession     // playerID → session (gameID + playerNum)

	timerMu    sync.Mutex
	turnTimers map[string]*turnTimerInfo // gameID → active turn timer
}

// NewGameRelay は GameRelay を生成します
func NewGameRelay(hub *ConnectionHub, battleClient service.BattleClient) *GameRelay {
	return &GameRelay{
		hub:          hub,
		battleClient: battleClient,
		gameMembers:  make(map[string][]string),
		playerGames:  make(map[string]playerSession),
		turnTimers:   make(map[string]*turnTimerInfo),
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
	r.sendToOpponent(playerID, gameID, constants.WSMsgOpponentDisconnected)
}

// NotifyOpponentReconnected は切断したプレイヤーの復帰時に対戦相手に opponent_reconnected を送信します
func (r *GameRelay) NotifyOpponentReconnected(playerID, gameID string) {
	r.sendToOpponent(playerID, gameID, constants.WSMsgOpponentReconnected)
}

// sendToOpponent は gameMembers から対戦相手を解決しメッセージを送信する。
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

// SendGameStateToPlayers はゲーム内の全プレイヤーにゲーム状態を送信します
func (r *GameRelay) SendGameStateToPlayers(gameID string) {
	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	var activePlayerID string
	var activeTimeBank int64
	var spectateState json.RawMessage // 観戦者に送る正規オブザーバービュー

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, pid := range players {
		pNum := r.resolvePlayerNum(pid)
		if pNum == 0 {
			continue
		}
		state, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pNum)
		if err != nil {
			log.Printf("get game state for player %s: %v", pid, err)
			sendErrorToPlayer(r.hub, pid, "game_state_error", "failed to retrieve game state", true)
			continue
		}

		// battle 状態からターンタイマー情報を抽出
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

		// 最初のプレイヤーの状態を観戦者用にキャプチャ
		if i == 0 {
			spectateState = state
		}
	}

	// アクティブプレイヤーの TimeBank に基づきターンタイマーを更新
	if activePlayerID != "" {
		r.resetTurnTimer(gameID, activePlayerID, activeTimeBank)
	}

	// 観戦者に状態更新を転送
	if r.spectateRelay != nil && spectateState != nil {
		r.spectateRelay.BroadcastStateUpdate(gameID, spectateState)
	}
}

// SendTurnControlsToPlayers はゲーム内の全プレイヤーにターンコントロール情報を送信します
func (r *GameRelay) SendTurnControlsToPlayers(gameID string) {
	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pid := range players {
		pNum := r.resolvePlayerNum(pid)
		if pNum == 0 {
			continue
		}
		raw, err := r.battleClient.GetTurnControlsForPlayer(ctx, gameID, pNum)
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

// sendActionPerformed はイベントごとに action_performed メッセージをディスパッチする。
//
// ルーティングはイベントメタデータに基づく:
//   - システムイベント (turn_start) → 全プレイヤーに送信
//   - 自分のアクション → 対戦相手に送信（情報隠蔽済み状態を取得）
//   - 他プレイヤーのアクション (NPC) → 行動プレイヤーに送信（battle server の状態付き）
func (r *GameRelay) sendActionPerformed(gameID, actingPlayerID string, result *service.ActionResult) {
	if result == nil || len(result.Events) == 0 {
		return
	}

	r.mu.RLock()
	players := r.gameMembers[gameID]
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	actingPlayerNum := r.resolvePlayerNum(actingPlayerID)
	for _, evt := range result.Events {
		switch {
		case evt.EventType == constants.EventTypeTurnStart:
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

// runNpcTurns は battle server が NpcPending=true を返す間 AdvanceNpcTurn を繰り返し呼び出す。
// 各イテレーションのイベントは sendActionPerformed で中継され、プレイヤーは全 NPC アクションを順に受け取る。
// battle server が NpcPending をクリアしないバグへの安全弁として maxNpcTurnIterations で上限を設ける。
func (r *GameRelay) runNpcTurns(ctx context.Context, gameID, playerID string, current *service.ActionResult) *service.ActionResult {
	const maxNpcTurnIterations = 200
	for i := 0; current != nil && current.NpcPending && !current.GameOver; i++ {
		if i >= maxNpcTurnIterations {
			log.Printf("runNpcTurns: iteration cap reached (game=%s, player=%s)", gameID, playerID)
			return current
		}
		next, err := r.battleClient.AdvanceNpcTurn(ctx, gameID)
		if err != nil {
			log.Printf("advance NPC turn loop (game=%s, player=%s): %v", gameID, playerID, err)
			return current
		}
		r.sendActionPerformed(gameID, playerID, next)
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

func (r *GameRelay) broadcastGameOver(gameID string, winningPlayerNum int64, reason string) {
	r.BroadcastToGame(gameID, &WSMessage{
		Type: constants.WSMsgGameOver,
		Data: mustMarshal(GameOverMessage{
			GameID:           gameID,
			WinningPlayerNum: winningPlayerNum,
			WinReason:        reason,
		}),
	})

	r.awardGameExp(gameID, winningPlayerNum, reason)

	// 観戦者に通知しクリーンアップ
	if r.spectateRelay != nil {
		r.spectateRelay.UnregisterGame(gameID, winningPlayerNum, reason)
	}
}

// awardGameExp はゲーム終了後にプレイヤーに経験値を付与する。
// インメモリ状態ではなく DB レベルの冪等性（exp_awarded フラグ）を使用し、
// 重複呼び出しや gateway 再起動による二重付与を防止する。
func (r *GameRelay) awardGameExp(gameID string, winnerNum int64, reason string) {
	if r.accountClient == nil || r.gamePlayerRepo == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	awarded, err := r.gamePlayerRepo.MarkExpAwarded(ctx, gameID)
	if err != nil {
		log.Printf("ERROR: mark exp awarded for game %s: %v", gameID, err)
		return
	}
	if !awarded {
		return
	}

	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		log.Printf("ERROR: lookup game players for exp (game %s): %v", gameID, err)
		return
	}

	var player1ID, player2ID string
	for _, e := range entries {
		switch e.PlayerNum {
		case 1:
			player1ID = e.PlayerID
		case 2:
			player2ID = e.PlayerID
		}
	}

	matchType := constants.MatchTypePvp
	if len(entries) == 1 {
		matchType = constants.MatchTypeNpc
	}

	if err := r.accountClient.AwardGameExp(ctx, player1ID, player2ID, winnerNum, reason, matchType); err != nil {
		log.Printf("ERROR: award game exp for game %s: %v", gameID, err)
	}
}

// HandleGameEnter は game_enter メッセージを処理します
func (r *GameRelay) HandleGameEnter(conn *Connection, data json.RawMessage) {
	var req GameEnterMessage
	if err := json.Unmarshal(data, &req); err != nil {
		sendError(conn, "invalid_data", "invalid game_enter data", false)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	playerNum, err := r.gamePlayerRepo.LookupPlayerNum(ctx, req.GameID, conn.playerID)
	if err != nil {
		log.Printf("lookup player_num for %s in game %s: %v", conn.playerID, req.GameID, err)
		sendError(conn, "game_error", "player not found in game", false)
		return
	}

	r.JoinGame(conn.playerID, req.GameID, playerNum)
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgGameEntered,
		Data: mustMarshal(map[string]string{"game_id": req.GameID}),
	})

	// battle_start/turn_start はエントリーアニメーション用の初期状態を運ぶ。
	// 続く SendGameStateToPlayers が権威ある状態を配信しターンタイマーを開始する。
	r.sendBattleStartAndTurnStart(conn, req.GameID)
	r.advanceNpcIfNeeded(req.GameID, conn.playerID)
	r.SendGameStateToPlayers(req.GameID)
	r.SendTurnControlsToPlayers(req.GameID)
}

// advanceNpcIfNeeded は NPC がアクティブプレイヤーの場合に NPC の最初のターンを実行する。
// game_enter 後に NPC アクションイベントが配信されるようにする。
func (r *GameRelay) advanceNpcIfNeeded(gameID, playerID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	matchType := r.lookupMatchType(ctx, gameID)
	if matchType != constants.MatchTypeNpc {
		return
	}

	result, err := r.battleClient.AdvanceNpcTurn(ctx, gameID)
	if err != nil {
		log.Printf("advance NPC turn (game %s): %v", gameID, err)
		return
	}

	r.sendActionPerformed(gameID, playerID, result)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pNum := r.resolvePlayerNum(conn.playerID)
	if pNum == 0 {
		log.Printf("cannot resolve player_num for %s in game %s", conn.playerID, gameID)
		return
	}
	rawState, err := r.battleClient.GetGameStateForPlayer(ctx, gameID, pNum)
	if err != nil {
		log.Printf("get game state for battle_start (player %s): %v", conn.playerID, err)
		sendErrorToPlayer(r.hub, conn.playerID, "game_state_error", "failed to retrieve game state", true)
		return
	}

	// DB からゲームメタデータを解決
	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		log.Printf("lookup game players for battle_start (game %s): %v", gameID, err)
		return
	}
	matchType := constants.MatchTypePvp
	if len(entries) == 1 {
		matchType = constants.MatchTypeNpc
	}

	battleStartData := map[string]interface{}{
		"match_type": matchType,
	}

	myName, myLevel := r.lookupPlayer(ctx, conn.playerID)
	var oppName string
	var oppLevel int64
	if matchType == constants.MatchTypeNpc {
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
		Type: constants.WSMsgActionPerformed,
		Data: mustMarshal(ActionPerformedMessage{
			Sequence:   0,
			ActionType: constants.EventTypeBattleStart,
			ActionData: mustMarshal(battleStartData),
			State:      rawState,
		}),
	})

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

// lookupPlayer はプレイヤーの表示名とレベルを解決する。エラー時はデフォルト値を返す。
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

	// action_performed はクライアントアニメーション用のスナップショットを運ぶ。
	// 続く SendGameStateToPlayers が権威ある最終状態を配信しターンタイマーをリセットする。
	r.sendActionPerformed(action.GameID, conn.playerID, result)
	// プレイヤーのアクションで NPC にターンが渡った場合、権威ある状態の前に全 NPC イベントを配信
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

// resetTurnTimer は既存のタイマーをキャンセルし新しいタイマーを開始する。
// タイマー発火時にアクティブプレイヤーの forfeit（タイムアウト負け）を送信する。
func (r *GameRelay) resetTurnTimer(gameID, activePlayerID string, timeBankSeconds int64) {
	r.timerMu.Lock()
	defer r.timerMu.Unlock()

	if info, ok := r.turnTimers[gameID]; ok {
		info.timer.Stop()
		delete(r.turnTimers, gameID)
	}

	if timeBankSeconds <= 0 {
		return
	}

	// ネットワーク遅延を考慮して 2 秒のバッファを追加。
	// battle server が権威ある情報源であり、Gateway タイマー発火直前に
	// プレイヤーがアクションを送信した場合、server は実経過時間を差し引き
	// アクションを許可する可能性がある。
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

// cancelTurnTimer はゲームのターンタイマーを停止し削除する。
func (r *GameRelay) cancelTurnTimer(gameID string) {
	r.timerMu.Lock()
	defer r.timerMu.Unlock()

	if info, ok := r.turnTimers[gameID]; ok {
		info.timer.Stop()
		delete(r.turnTimers, gameID)
	}
}

// handleTurnTimeout はターンタイマー期限切れ時に forfeit アクションを送信する。
//
// forfeit reason は本来バトルのドメイン知識だが、ターンタイマーと切断タイマーは
// gateway の責務であり、Battle Server はタイムアウトの種別を区別できないため、
// 例外的に gateway が reason を指定して Battle Server に送る。
// broadcastGameOver でも gateway 側の WinReason で上書きしている。
func (r *GameRelay) handleTurnTimeout(gameID, playerID string) {
	pNum := r.resolvePlayerNum(playerID)
	if pNum == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := r.battleClient.ProcessAction(ctx, gameID, pNum, constants.ActionTypeForfeit, forfeitReason(constants.WinReasonTurnTimeout))
	if err != nil {
		log.Printf("turn timeout forfeit error (game=%s, player=%s): %v", gameID, playerID, err)
		return
	}
	if result != nil && result.GameOver {
		r.SendGameStateToPlayers(gameID)
		r.broadcastGameOver(gameID, result.WinningPlayerNum, constants.WinReasonTurnTimeout)
		r.leaveAllPlayers(gameID)
	}
}

// HandleUseStamp は use_stamp メッセージを処理します
func (r *GameRelay) HandleUseStamp(conn *Connection, data json.RawMessage) {
	var req UseStampMessage
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}
	pNum := r.resolvePlayerNum(conn.playerID)
	r.BroadcastToGame(req.GameID, &WSMessage{
		Type: constants.WSMsgStampUsed,
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := r.battleClient.ProcessAction(ctx, gameID, pNum, constants.ActionTypeForfeit, forfeitReason(constants.WinReasonDisconnect))
	if err != nil {
		log.Printf("forfeit error: %v", err)
		return
	}
	if result != nil && result.GameOver {
		r.broadcastGameOver(gameID, result.WinningPlayerNum, constants.WinReasonDisconnect)
		r.leaveAllPlayers(gameID)
	}
}

// NotifyMatchFound は両プレイヤーに match_found を送信します
func (r *GameRelay) NotifyMatchFound(gameID, player1ID, player2ID string) {
	msg := &WSMessage{
		Type: constants.WSMsgMatchFound,
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
func (r *GameRelay) lookupMatchType(ctx context.Context, gameID string) string {
	if r.gamePlayerRepo == nil {
		return ""
	}
	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		log.Printf("lookup match type for game %s: %v", gameID, err)
		return ""
	}
	if len(entries) == 1 {
		return constants.MatchTypeNpc
	}
	return constants.MatchTypePvp
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
