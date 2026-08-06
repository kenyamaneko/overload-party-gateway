package ws

import (
	"context"
	"errors"
	"log/slog"
	"time"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
)

// turnTimerInfo はアクティブなターンタイマーの状態を保持する。
type turnTimerInfo struct {
	timer          Timer
	activePlayerID string
}

// turnTimerNetworkBuffer は battle server とのネットワーク遅延を吸収するための加算値。
// battle server が権威ある情報源であり、Gateway タイマー発火直前にプレイヤーが
// アクションを送信した場合、server は実経過時間を差し引きアクションを許可する可能性がある。
const turnTimerNetworkBuffer = 2 * time.Second

// resetTurnTimer は既存のタイマーをキャンセルし新しいタイマーを開始する。
// タイマー発火時にアクティブプレイヤーの forfeit（タイムアウト負け）を送信する。
func (r *GameRelay) resetTurnTimer(gameID, activePlayerID string, timeBankSeconds int64) {
	r.timerMu.Lock()
	if info, ok := r.turnTimers[gameID]; ok {
		info.timer.Stop()
		delete(r.turnTimers, gameID)
	}

	if timeBankSeconds <= 0 {
		r.timerMu.Unlock()
		return
	}

	duration := time.Duration(timeBankSeconds)*time.Second + turnTimerNetworkBuffer

	timer := r.clock.AfterFunc(duration, func() {
		r.timerMu.Lock()
		info, ok := r.turnTimers[gameID]
		if !ok || info.activePlayerID != activePlayerID {
			// ターン交代済み — 旧プレイヤーへの誤 forfeit を防止
			r.timerMu.Unlock()
			return
		}
		delete(r.turnTimers, gameID)
		r.timerMu.Unlock()

		slog.Info("turn timer expired", "game_id", gameID, "player_id", activePlayerID)
		r.handleTurnTimeout(gameID, activePlayerID)
	})

	r.turnTimers[gameID] = &turnTimerInfo{
		timer:          timer,
		activePlayerID: activePlayerID,
	}
	r.timerMu.Unlock()
}

// cancelTurnTimer はゲームのターンタイマーを停止し削除する。
func (r *GameRelay) cancelTurnTimer(gameID string) {
	r.timerMu.Lock()
	if info, ok := r.turnTimers[gameID]; ok {
		info.timer.Stop()
		delete(r.turnTimers, gameID)
	}
	r.timerMu.Unlock()
}

// handleTurnTimeout はターンタイマー期限切れ時に forfeit アクションを送信する。
//
// forfeit reason は本来バトルのドメイン知識だが、ターンタイマーと切断タイマーは
// gateway の責務であり、Battle Server はタイムアウトの種別を区別できないため、
// 例外的に gateway が reason を指定して Battle Server に送る。
// broadcastGameOver でも gateway 側の WinReason で上書きしている。
//
// 対戦相手も切断中の場合はこの時点では決着させない (HandleDisconnectTimeout と同じ
// 理由)。次にどちらかが戻ってきた時点で resolveStaleDisconnect が改めて評価する。
func (r *GameRelay) handleTurnTimeout(gameID, playerID string) {
	ctx, cancel := context.WithTimeout(context.Background(), downstreamCallTimeout)
	bothDisconnected, err := r.areBothPlayersDisconnected(ctx, playerID, gameID)
	cancel()
	if err != nil {
		slog.Error("turn timeout: lookup game players failed", "game_id", gameID, "player_id", playerID, "error", err)
		return
	}
	if bothDisconnected {
		slog.Info("turn timer expired while opponent is also disconnected, deferring resolution", "player_id", playerID, "game_id", gameID)
		return
	}

	// タイマー発火は WS コネクション context に紐づかない（接続が生きていても発火する）。
	// Background ベースで独立した短いタイムアウトを使う。
	ctx2, cancel2 := context.WithTimeout(context.Background(), downstreamCallTimeout)
	defer cancel2()
	pNum, err := r.resolvePlayerNum(ctx2, gameID, playerID)
	if err != nil {
		slog.Error("turn timeout: resolve player_num failed, game left unresolved", "game_id", gameID, "player_id", playerID, "error", err)
		return
	}
	result, err := r.battleClient.ProcessAction(ctx2, gameID, pNum, gamelogic.ActionTypeForfeit, buildForfeitReason(gamelogic.WinReasonTurnTimeout))
	if err != nil {
		slog.Error("turn timeout forfeit failed", "game_id", gameID, "player_id", playerID, "error", err)
		// forfeit が battle server に到達しないとゲームが終了せず、両プレイヤーが
		// 待ち続けてしまう。両プレイヤーに通知し、回復は再接続/監視に委ねる。
		sendErrorToPlayer(r.hub, playerID, "turn_timeout_failed", "failed to register turn timeout", true)
		return
	}
	if result != nil && result.GameOver {
		r.SendGameStateToPlayers(gameID)
		r.broadcastGameOver(gameID, result.WinningPlayerNum, gamelogic.WinReasonTurnTimeout)
		r.leaveAllPlayers(gameID)
	}
}

// isCanceled は context キャンセル/期限切れによる中断かを判定する。
// 上流（WS 切断）の停止に伴うエラーをユーザー向けの「失敗」として扱わないために使う。
func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
