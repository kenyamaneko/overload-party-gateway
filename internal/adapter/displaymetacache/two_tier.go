package displaymetacache

import (
	"context"
	"errors"
	"fmt"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// l1Tier は L1 が満たす内部契約。Put / Get に加え、L2 書き込み失敗時の
// 巻き戻しのため Evict を必須にする。
type l1Tier interface {
	port.DisplayMetaStore
	port.DisplayMetaLookup
	Evict(ctx context.Context, gameID string, playerNum int) error
}

// l2Tier は L2 が満たす内部契約。Put / Get のみ。
type l2Tier interface {
	port.DisplayMetaStore
	port.DisplayMetaLookup
}

// TwoTier は L1 (pod-local in-memory) と L2 (Upstash Redis) を合成した
// 2 段キャッシュ。Get は L1 を優先し、miss 時のみ L2 を参照して L1 へ昇格
// させる。Put は L1 / L2 双方へ書き込み、L2 失敗時は L1 を巻き戻す。
type TwoTier struct {
	l1 l1Tier
	l2 l2Tier
}

// New は L1 / L2 を合成して TwoTier を生成する。
func New(l1 l1Tier, l2 l2Tier) *TwoTier {
	return &TwoTier{l1: l1, l2: l2}
}

// Put は L1 / L2 双方へ snapshot を書き込む。L2 失敗時は L1 を Evict して
// 巻き戻し、error を呼び出し側に伝播させる。
func (c *TwoTier) Put(ctx context.Context, gameID string, playerNum int, meta port.DisplayMeta) error {
	if err := c.l1.Put(ctx, gameID, playerNum, meta); err != nil {
		return fmt.Errorf("l1 put: %w", err)
	}
	if err := c.l2.Put(ctx, gameID, playerNum, meta); err != nil {
		// L1 だけが snapshot を持つ非対称状態 (pod ローカルでは見えるが
		// 他 pod / restart 後には消える) を残さないため L1 も巻き戻す。
		if evictErr := c.l1.Evict(ctx, gameID, playerNum); evictErr != nil {
			return fmt.Errorf("l2 put: %w (l1 evict also failed: %v)", err, evictErr)
		}
		return fmt.Errorf("l2 put: %w", err)
	}
	return nil
}

// Get は L1 を優先参照し、miss 時のみ L2 にフォールバックする。L2 hit 時は
// L1 へ昇格させ、昇格失敗時はその error を呼び出し側に返す。
func (c *TwoTier) Get(ctx context.Context, gameID string, playerNum int) (port.DisplayMeta, error) {
	meta, err := c.l1.Get(ctx, gameID, playerNum)
	if err == nil {
		return meta, nil
	}
	if !errors.Is(err, port.ErrNotFound) {
		return port.DisplayMeta{}, fmt.Errorf("l1 get: %w", err)
	}

	meta, err = c.l2.Get(ctx, gameID, playerNum)
	if err != nil {
		return port.DisplayMeta{}, fmt.Errorf("l2 get: %w", err)
	}
	if err := c.l1.Put(ctx, gameID, playerNum, meta); err != nil {
		return port.DisplayMeta{}, fmt.Errorf("l1 promote: %w", err)
	}
	return meta, nil
}
