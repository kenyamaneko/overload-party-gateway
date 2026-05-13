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
	if r.cache != nil {
		meta, err := r.cache.Get(ctx, gameID, playerNum)
		if err == nil {
			return meta
		}
		if errors.Is(err, port.ErrNotFound) {
			slog.Warn("displayresolver: cache miss", "game_id", gameID, "player_num", playerNum)
		} else {
			slog.Error("displayresolver: cache read failed", "game_id", gameID, "player_num", playerNum, "error", err)
		}
	}

	if r.getter != nil {
		profile, err := r.getter.GetPlayerProfile(ctx, playerID)
		switch {
		case err != nil:
			slog.Error("displayresolver: account lookup failed", "player_id", playerID, "error", err)
		case profile.Name == "":
			slog.Error("displayresolver: account returned profile with empty name", "player_id", playerID)
		default:
			meta := port.DisplayMeta(profile)
			if r.cache != nil {
				if err := r.cache.Put(ctx, gameID, playerNum, meta); err != nil {
					slog.Error("displayresolver: cache write-back failed", "game_id", gameID, "player_num", playerNum, "error", err)
				}
			}
			return meta
		}
	}

	meta := fallbackDisplayMeta(playerID)
	if r.cache != nil {
		if err := r.cache.Put(ctx, gameID, playerNum, meta); err != nil {
			slog.Error("displayresolver: fallback write failed", "game_id", gameID, "player_num", playerNum, "error", err)
		}
	}
	return meta
}

// fallbackDisplayMeta は account から表示可能な name が得られない場合の代替値を返す。
// 明示文字列にすることで「空文字 silent fallback」を避ける。
func fallbackDisplayMeta(playerID string) port.DisplayMeta {
	prefix := playerID
	if len(prefix) > fallbackDisplayNamePrefixLen {
		prefix = prefix[:fallbackDisplayNamePrefixLen]
	}
	return port.DisplayMeta{Name: "Player " + prefix, Level: 0}
}
