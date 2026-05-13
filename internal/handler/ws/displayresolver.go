package ws

import (
	"context"
	"errors"
	"log"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// fallbackDisplayNamePrefixLen は playerID を「Player {prefix}」形式に短縮するときの
// prefix 長。UI 上で取得失敗ケースを目視判別可能にする最小桁数の目安。
const fallbackDisplayNamePrefixLen = 6

// displayCache は DisplayResolver が依存する read/write 兼用キャッシュ境界。
// L1+L2 を合成した実装 (displaymetacache.TwoTier) を想定する。
type displayCache interface {
	port.DisplayMetaStore
	port.DisplayMetaLookup
}

// playerLookuper は account から player profile を取得する境界。
// accountclient.Client が満たす最小契約のみを抜き出して resolver の依存を限定する。
type playerLookuper interface {
	GetPlayer(ctx context.Context, playerID string) (*apiaccount.PlayerResponse, error)
}

// DisplayResolver は port.DisplayResolver の標準実装。
type DisplayResolver struct {
	cache    displayCache
	lookuper playerLookuper
}

// NewDisplayResolver は displayCache と player lookuper から resolver を生成する。
func NewDisplayResolver(cache displayCache, lookuper playerLookuper) *DisplayResolver {
	return &DisplayResolver{cache: cache, lookuper: lookuper}
}

// Resolve は (gameID, playerNum, playerID) に対応する DisplayMeta を返す。
// cache hit → cache miss/read 失敗時は account 直接 lookup → 両方失敗時は
// フォールバック表示値を Redis に書き込みかつ返す、という 3 段で必ず値を返す。
func (r *DisplayResolver) Resolve(ctx context.Context, gameID string, playerNum int, playerID string) port.DisplayMeta {
	if meta, ok := r.tryCache(ctx, gameID, playerNum); ok {
		return meta
	}
	if meta, ok := r.tryAccount(ctx, gameID, playerNum, playerID); ok {
		return meta
	}
	return r.writeFallback(ctx, gameID, playerNum, playerID)
}

// tryCache は cache を参照して hit すれば DisplayMeta を返す。miss は ok=false、
// read エラーは Error ログを残して miss 扱いにする。
func (r *DisplayResolver) tryCache(ctx context.Context, gameID string, playerNum int) (port.DisplayMeta, bool) {
	if r.cache == nil {
		return port.DisplayMeta{}, false
	}
	meta, err := r.cache.Get(ctx, gameID, playerNum)
	if err == nil {
		return meta, true
	}
	if errors.Is(err, port.ErrNotFound) {
		log.Printf("WARN: displayresolver: cache miss game=%s player_num=%d (TTL exceeded or unwritten)", gameID, playerNum)
		return port.DisplayMeta{}, false
	}
	log.Printf("ERROR: displayresolver: cache read failed game=%s player_num=%d: %v", gameID, playerNum, err)
	return port.DisplayMeta{}, false
}

// tryAccount は account から player profile を取り、name が確定していれば
// 結果を cache に書き戻して DisplayMeta を返す。失敗時は ok=false。
func (r *DisplayResolver) tryAccount(ctx context.Context, gameID string, playerNum int, playerID string) (port.DisplayMeta, bool) {
	if r.lookuper == nil {
		return port.DisplayMeta{}, false
	}
	p, err := r.lookuper.GetPlayer(ctx, playerID)
	if err != nil {
		log.Printf("ERROR: displayresolver: account lookup failed player=%s: %v", playerID, err)
		return port.DisplayMeta{}, false
	}
	if p == nil || p.Name == nil {
		log.Printf("ERROR: displayresolver: account returned incomplete profile for player=%s", playerID)
		return port.DisplayMeta{}, false
	}
	meta := port.DisplayMeta{Name: *p.Name, Level: int(p.Level)}
	if r.cache != nil {
		if err := r.cache.Put(ctx, gameID, playerNum, meta); err != nil {
			log.Printf("ERROR: displayresolver: cache write-back failed game=%s player_num=%d: %v", gameID, playerNum, err)
		}
	}
	return meta, true
}

// writeFallback はフォールバック表示値を cache に書き込み (best-effort) 同値を返す。
// 後続の relay 経路で毎回 account を再 lookup しないための前倒し書き込み。
func (r *DisplayResolver) writeFallback(ctx context.Context, gameID string, playerNum int, playerID string) port.DisplayMeta {
	meta := fallbackDisplayMeta(playerID)
	if r.cache == nil {
		return meta
	}
	if err := r.cache.Put(ctx, gameID, playerNum, meta); err != nil {
		log.Printf("ERROR: displayresolver: fallback write failed game=%s player_num=%d: %v", gameID, playerNum, err)
	}
	return meta
}

// fallbackDisplayMeta は account から表示可能な name が得られない場合の代替値。
// 「空文字での silent fallback」を避け、UI 上「取得失敗が起きている」と判別可能な
// 明示文字列を返す。
func fallbackDisplayMeta(playerID string) port.DisplayMeta {
	short := playerID
	if len(short) > fallbackDisplayNamePrefixLen {
		short = short[:fallbackDisplayNamePrefixLen]
	}
	return port.DisplayMeta{Name: "Player " + short, Level: 0}
}
