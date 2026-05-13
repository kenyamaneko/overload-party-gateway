package ws

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// fallbackDisplayNamePrefixLen は playerID を「Player {prefix}」形式に短縮するときの
// prefix 長。UI 上で取得失敗ケースを目視判別可能にする最小桁数の目安。
const fallbackDisplayNamePrefixLen = 6

// displayCache は DisplayResolver が依存する read/write 兼用キャッシュ境界。
type displayCache interface {
	port.DisplayMetaStore
	port.DisplayMetaLookup
}

// DisplayResolver は port.DisplayResolver の標準実装。
type DisplayResolver struct {
	cache  displayCache
	getter port.PlayerProfileGetter
}

// NewDisplayResolver は cache と PlayerProfileGetter から resolver を生成する。
func NewDisplayResolver(cache displayCache, getter port.PlayerProfileGetter) *DisplayResolver {
	return &DisplayResolver{cache: cache, getter: getter}
}

// Resolve は (gameID, playerNum, playerID) に対応する DisplayMeta を返す。常に
// 表示可能な値を返す保証は port.DisplayResolver 契約で規定される。
func (r *DisplayResolver) Resolve(ctx context.Context, gameID string, playerNum int, playerID string) port.DisplayMeta {
	if meta, ok := r.tryCache(ctx, gameID, playerNum); ok {
		return meta
	}
	if meta, ok := r.tryAccount(ctx, gameID, playerNum, playerID); ok {
		return meta
	}
	return r.writeFallback(ctx, gameID, playerNum, playerID)
}

// tryCache は cache を参照して hit すれば DisplayMeta を返す。
func (r *DisplayResolver) tryCache(ctx context.Context, gameID string, playerNum int) (port.DisplayMeta, bool) {
	if r.cache == nil {
		return port.DisplayMeta{}, false
	}
	meta, err := r.cache.Get(ctx, gameID, playerNum)
	if err == nil {
		return meta, true
	}
	if errors.Is(err, port.ErrNotFound) {
		slog.Warn("displayresolver: cache miss", "game_id", gameID, "player_num", playerNum)
		return port.DisplayMeta{}, false
	}
	slog.Error("displayresolver: cache read failed", "game_id", gameID, "player_num", playerNum, "error", err)
	return port.DisplayMeta{}, false
}

// tryAccount は account から player profile を取り、name が確定していれば DisplayMeta を返す。
func (r *DisplayResolver) tryAccount(ctx context.Context, gameID string, playerNum int, playerID string) (port.DisplayMeta, bool) {
	if r.getter == nil {
		return port.DisplayMeta{}, false
	}
	profile, err := r.getter.GetPlayerProfile(ctx, playerID)
	if err != nil {
		slog.Error("displayresolver: account lookup failed", "player_id", playerID, "error", err)
		return port.DisplayMeta{}, false
	}
	if profile.Name == "" {
		slog.Error("displayresolver: account returned profile with empty name", "player_id", playerID)
		return port.DisplayMeta{}, false
	}
	meta := port.DisplayMeta{Name: profile.Name, Level: profile.Level}
	if r.cache != nil {
		if err := r.cache.Put(ctx, gameID, playerNum, meta); err != nil {
			slog.Error("displayresolver: cache write-back failed", "game_id", gameID, "player_num", playerNum, "error", err)
		}
	}
	return meta, true
}

// writeFallback はフォールバック表示値を cache に書き込み (best-effort) 同値を返す。
func (r *DisplayResolver) writeFallback(ctx context.Context, gameID string, playerNum int, playerID string) port.DisplayMeta {
	meta := fallbackDisplayMeta(playerID)
	if r.cache == nil {
		return meta
	}
	if err := r.cache.Put(ctx, gameID, playerNum, meta); err != nil {
		slog.Error("displayresolver: fallback write failed", "game_id", gameID, "player_num", playerNum, "error", err)
	}
	return meta
}

// fallbackDisplayMeta は account から表示可能な name が得られない場合の代替値を返す。
// 明示文字列にすることで「空文字 silent fallback」を避ける。
func fallbackDisplayMeta(playerID string) port.DisplayMeta {
	short := playerID
	if len(short) > fallbackDisplayNamePrefixLen {
		short = short[:fallbackDisplayNamePrefixLen]
	}
	return port.DisplayMeta{Name: "Player " + short, Level: 0}
}
