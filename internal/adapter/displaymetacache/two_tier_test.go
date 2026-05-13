package displaymetacache

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// newTestTwoTier は MemoryStore (L1) + miniredis backed RedisStore (L2) を
// 合成した TwoTier を構築する。Redis 側を生で叩いて L2 のみへの仕込み等を
// 行いたいため miniredis と RedisStore も返す。
func newTestTwoTier(t *testing.T) (*TwoTier, *MemoryStore, *RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l1 := NewMemoryStore()
	l2 := NewRedisStore(client)
	return New(l1, l2), l1, l2, mr
}

// TestTwoTier_Put_WritesBothLayers は Put が L1 / L2 双方に snapshot を
// 書き込むことを固定する (Set: L1 と L2 両方)。
func TestTwoTier_Put_WritesBothLayers(t *testing.T) {
	tier, l1, l2, _ := newTestTwoTier(t)
	ctx := context.Background()

	require.NoError(t, tier.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))

	gotL1, err := l1.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, gotL1)

	gotL2, err := l2.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, gotL2)
}

// TestTwoTier_Get_L1Hit は L1 に snapshot が存在するとき L2 を参照せずに
// L1 の値を返すことを固定する (Redis read 抑制によるコスト対策)。
func TestTwoTier_Get_L1Hit(t *testing.T) {
	tier, l1, _, mr := newTestTwoTier(t)
	ctx := context.Background()

	require.NoError(t, l1.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
	// L2 (Redis) を停止しても L1 hit の場合は影響を受けない
	mr.Close()

	got, err := tier.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)
}

// TestTwoTier_Get_L2HitPromotesToL1 は L1 miss / L2 hit のとき値を返した
// 上で L1 に昇格し、次回以降は L1 で hit することを固定する。
func TestTwoTier_Get_L2HitPromotesToL1(t *testing.T) {
	tier, l1, l2, _ := newTestTwoTier(t)
	ctx := context.Background()

	require.NoError(t, l2.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))

	// 1 回目: L1 miss → L2 hit → 値返却 + L1 へ昇格
	got, err := tier.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)

	gotL1, err := l1.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, gotL1, "L2 hit 後は L1 にも乗っている")
}

// TestTwoTier_Get_BothMissReturnsNotFound は L1 / L2 双方に snapshot が
// ないとき port.ErrNotFound が伝播することを固定する (silent fallback 禁止)。
func TestTwoTier_Get_BothMissReturnsNotFound(t *testing.T) {
	tier, _, _, _ := newTestTwoTier(t)

	_, err := tier.Get(context.Background(), "g-missing", 1)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

// TestTwoTier_Put_L2FailureRollsBackL1 は L2 書き込み失敗時に L1 が
// 巻き戻されて L2 と整合し、error が呼び出し側に伝播することを固定する
// (silent fallback 禁止 + 「pod ローカルだけ見える」非対称状態の回避)。
func TestTwoTier_Put_L2FailureRollsBackL1(t *testing.T) {
	tier, l1, _, mr := newTestTwoTier(t)
	ctx := context.Background()

	mr.Close() // L2 を停止して書き込み失敗を強制する

	err := tier.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7})
	require.Error(t, err)

	_, getErr := l1.Get(ctx, "g1", 1)
	assert.ErrorIs(t, getErr, port.ErrNotFound, "L2 失敗時は L1 も巻き戻されている")
}

// TestTwoTier_Get_L1NonNotFoundErrorPropagates は L1 が ErrNotFound 以外の
// error を返したとき、L2 にフォールバックせずそのまま error を伝播させる
// 仕様を固定する (異常を握りつぶさない)。
func TestTwoTier_Get_L1NonNotFoundErrorPropagates(t *testing.T) {
	l1 := &stubL1{err: errors.New("l1 boom")}
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l2 := NewRedisStore(client)

	tier := New(l1, l2)
	require.NoError(t, l2.Put(context.Background(), "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))

	_, err := tier.Get(context.Background(), "g1", 1)
	require.Error(t, err)
	assert.NotErrorIs(t, err, port.ErrNotFound)
}

// TestTwoTier_Get_L1PromoteFailurePropagates は L2 hit 後の L1 昇格が失敗
// したとき、error が呼び出し側に伝播することを固定する (silent な握りつぶし
// 禁止: 後続 PR でフォールバック判断するため呼び出し側に渡す)。
func TestTwoTier_Get_L1PromoteFailurePropagates(t *testing.T) {
	l1 := &stubL1{
		getErr: port.ErrNotFound,
		putErr: errors.New("l1 promote boom"),
	}
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l2 := NewRedisStore(client)

	tier := New(l1, l2)
	require.NoError(t, l2.Put(context.Background(), "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))

	_, err := tier.Get(context.Background(), "g1", 1)
	require.Error(t, err)
	assert.NotErrorIs(t, err, port.ErrNotFound)
}

// stubL1 は L1 の各メソッドが任意の error を返せるテスト用 store。
// l1Tier interface を満たす最小実装。
type stubL1 struct {
	err    error // 非 nil なら全メソッドが共通して返す
	getErr error // Get 専用 (err より優先)
	putErr error // Put 専用 (err より優先)
}

func (s *stubL1) Put(_ context.Context, _ string, _ int, _ port.DisplayMeta) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.err
}

func (s *stubL1) Get(_ context.Context, _ string, _ int) (port.DisplayMeta, error) {
	if s.getErr != nil {
		return port.DisplayMeta{}, s.getErr
	}
	return port.DisplayMeta{}, s.err
}

func (s *stubL1) Evict(_ context.Context, _ string, _ int) error {
	return nil
}
