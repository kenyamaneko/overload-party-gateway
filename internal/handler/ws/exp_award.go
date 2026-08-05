package ws

import (
	"context"
	"log/slog"
	"time"

	gamedesign "github.com/kenyamaneko/overload-party-common/packages/game-design-constants"
)

// awardGameExp はゲーム終了後にプレイヤーに経験値を付与する。
//
// 冪等性は DB の `gateway.game_players.exp_awarded` フラグで担保する。
// MarkExpAwarded は最初に成功した呼び出しのみ awarded=true を返し、
// 後続の呼び出し（gateway 再起動・game_over 重複ブロードキャスト等）は false を返して
// 二重付与を防ぐ。**MarkExpAwarded を必ず先に実行する**ことで「フラグだけ立って
// 付与は失敗」なケースは生じても「付与だけ成功してフラグが立たない」ケース（→ 二重付与）
// は発生しない順序を保証する。
//
// awardGameExp は broadcastGameOver から（典型的にはゲーム終了パスから）呼ばれる。
// 親 ctx はその呼び出し元のもの（短命なリクエスト ctx の可能性がある）を引き継ぐが、
// game_over の DB 記録は接続切断後も完了させたいので Background 由来の独立タイムアウトを使う。
func (r *GameRelay) awardGameExp(gameID string, winnerNum int64, reason string) {
	if r.accountClient == nil || r.gamePlayerRepo == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	awarded, err := r.gamePlayerRepo.MarkExpAwarded(ctx, gameID)
	if err != nil {
		// 冪等フラグの書き込みに失敗。次回同じ game_id の awardGameExp が
		// 走った場合、再度 MarkExpAwarded が呼ばれて成功すれば付与が走り、
		// **二重付与のリスクがある**。運用で要監視 (severity=ERROR で検知)。
		slog.Error("mark exp awarded failed (idempotency at risk)", "game_id", gameID, "error", err)
		return
	}
	if !awarded {
		return
	}

	entries, err := r.gamePlayerRepo.LookupGamePlayers(ctx, gameID)
	if err != nil {
		// MarkExpAwarded は成功済み（フラグ立った）なので、再試行しても二重付与にはならないが
		// このゲームでの EXP は失われる。要監視。
		slog.Error("lookup game players for exp failed (exp lost)", "game_id", gameID, "error", err)
		return
	}

	player1ID, player2ID := playerIDsBySlot(entries)

	matchType := gamedesign.MatchTypePvp
	if len(entries) == 1 {
		matchType = gamedesign.MatchTypeNpc
	}

	if err := r.accountClient.AwardGameExp(ctx, player1ID, player2ID, winnerNum, reason, matchType); err != nil {
		// account への RPC 失敗。フラグは既に立っているので二重付与にはならないが
		// EXP は付与されない。要監視。
		slog.Error("award game exp failed (exp lost)", "game_id", gameID, "error", err)
	}
}
