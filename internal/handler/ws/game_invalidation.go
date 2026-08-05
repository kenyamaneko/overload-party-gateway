package ws

import (
	"context"
	"log/slog"
	"time"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
)

// pvpHumanSlots は PvP の対戦が持つ人間プレイヤーのスロット数。
const pvpHumanSlots = 2

// invalidationMarkTimeout は停止時に無効化の記録を書き込む上限時間。
// 強制終了までの猶予を WS の終了通知と分け合うため、短く区切る。
const invalidationMarkTimeout = 2 * time.Second

// forfeitBothPlayerNum は両者強制決着を要求するときに渡すスロット番号。
// 両者強制決着は勝者をスロット番号から決めないため、どの番号でも結果は変わらない。
const forfeitBothPlayerNum = 1

// gameInvalidatedErrorCode は無効になった対戦への参加を断るときの error_code。
const gameInvalidatedErrorCode = "game_invalidated"

// InvalidateActiveGames は進行中の対戦を無効として記録します。停止時に呼ばれます。
//
// 停止時に記録だけを残すのは、強制終了までの猶予が短く、対戦ごとに battle と account を
// 呼ぶ形では取りこぼすため。battle 上での決着は次の起動で RecoverInvalidatedGames が行う。
func (r *GameRelay) InvalidateActiveGames(ctx context.Context) {
	if r.invalidatedGameRepo == nil || r.gamePlayerRepo == nil {
		return
	}
	active := r.activeGameIDs()
	if len(active) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, invalidationMarkTimeout)
	defer cancel()

	targets, err := r.pvpGamesAmong(ctx, active)
	if err != nil {
		// 記録できなかった対戦は無効にならず、復帰したプレイヤーが切断猶予で評価される。要監視。
		slog.Error("shutdown invalidation: count players failed, games left valid", "game_count", len(active), "error", err)
		return
	}
	if len(targets) == 0 {
		return
	}
	if err := r.invalidatedGameRepo.MarkInvalidated(ctx, targets); err != nil {
		slog.Error("shutdown invalidation: mark invalidated failed, games left valid", "game_count", len(targets), "error", err)
		return
	}
	slog.Info("marked games invalidated on shutdown", "game_count", len(targets))
}

// pvpGamesAmong は gameIDs のうち、人間プレイヤーが 2 人いる対戦の ID を返す。
//
// NPC 戦を除くのは、対戦相手が居ない対戦には切断猶予の評価が働かず、停止をまたいでも
// そのまま再開できるため。
func (r *GameRelay) pvpGamesAmong(ctx context.Context, gameIDs []string) ([]string, error) {
	counts, err := r.gamePlayerRepo.CountPlayersByGame(ctx, gameIDs)
	if err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(gameIDs))
	for _, gameID := range gameIDs {
		if counts[gameID] == pvpHumanSlots {
			targets = append(targets, gameID)
		}
	}
	return targets, nil
}

// activeGameIDs は現在メンバーシップが残っている対戦の ID を返す。
// 決着した対戦は leaveAllPlayers でメンバーシップが解除されるため含まれない。
func (r *GameRelay) activeGameIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	gameIDs := make([]string, 0, len(r.gameMembers))
	for gameID := range r.gameMembers {
		gameIDs = append(gameIDs, gameID)
	}
	return gameIDs
}

// RecoverInvalidatedGames は無効として記録された対戦を battle 上で決着させます。起動時に呼ばれます。
func (r *GameRelay) RecoverInvalidatedGames(ctx context.Context) {
	if r.invalidatedGameRepo == nil {
		return
	}
	gameIDs, err := r.invalidatedGameRepo.ListUnfinished(ctx)
	if err != nil {
		slog.Error("invalidated game recovery: list failed", "error", err)
		return
	}
	if len(gameIDs) == 0 {
		return
	}
	slog.Info("recovering invalidated games", "game_count", len(gameIDs))
	for _, gameID := range gameIDs {
		r.finishInvalidatedGame(ctx, gameID)
	}
}

// finishInvalidatedGame は無効になった対戦を battle 上で決着させ、切断猶予の期限を消す。
//
// 決着が成功した対戦だけを決着済みとして記録するのは、battle が決着済みの対戦への
// 強制決着を拒むため。失敗した対戦は記録が残り、次の起動で改めて処理される。
func (r *GameRelay) finishInvalidatedGame(ctx context.Context, gameID string) {
	// 両者強制決着の理由は battle が決めるため、gateway からは何も渡さない。
	if _, err := r.battleClient.ProcessAction(ctx, gameID, forfeitBothPlayerNum, gamelogic.ActionTypeForfeitBoth, nil); err != nil {
		slog.Error("invalidated game recovery: forfeit failed, retried on next startup", "game_id", gameID, "error", err)
		return
	}
	if err := r.invalidatedGameRepo.MarkFinished(ctx, gameID); err != nil {
		slog.Error("invalidated game recovery: mark finished failed, forfeit retried on next startup", "game_id", gameID, "error", err)
		return
	}
	r.clearDisconnectDeadlines(ctx, gameID)
}

// clearDisconnectDeadlines は対戦の全プレイヤーの切断猶予期限の写しを削除する。
func (r *GameRelay) clearDisconnectDeadlines(ctx context.Context, gameID string) {
	if r.gamePlayerRepo == nil {
		return
	}
	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		slog.Error("invalidated game recovery: lookup players to clear deadlines failed", "game_id", gameID, "error", err)
		return
	}
	for _, e := range entries {
		r.hub.ClearDisconnectDeadline(e.PlayerID)
	}
}

// isGameInvalidated は対戦が無効として記録済みかを返す。記録先が未設定なら false を返す。
func (r *GameRelay) isGameInvalidated(ctx context.Context, gameID string) (bool, error) {
	if r.invalidatedGameRepo == nil {
		return false, nil
	}
	return r.invalidatedGameRepo.IsInvalidated(ctx, gameID)
}
