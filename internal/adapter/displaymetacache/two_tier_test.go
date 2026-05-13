package displaymetacache

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// newTestTwoTier は MemoryStore (L1) + miniredis backed RedisStore (L2) を合成した
// TwoTier を構築する。L1 / L2 / miniredis 本体も返し、layer ごとの状態確認 / L2 障害
// 注入に使う。MaxRetries=-1 は障害注入 (miniredis.Close 後) で retry のログを抑制するため。
func newTestTwoTier(t *testing.T) (*TwoTier, *MemoryStore, *RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr:       mr.Addr(),
		MaxRetries: -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	l1 := NewMemoryStore()
	l2 := NewRedisStore(client)
	return New(l1, l2), l1, l2, mr
}

// TestTwoTier_Put_WritesToBothLayers は Put が L1 / L2 双方に snapshot を
// 書き込むことを検証する (Put 成功時に L2 へも反映され、他 pod / pod restart 後の
// Get に備えていること)。
func TestTwoTier_Put_WritesToBothLayers(t *testing.T) {
	tier, l1, l2, _ := newTestTwoTier(t)
	ctx := context.Background()
	meta := port.DisplayMeta{Name: "alice", Level: 7}
	require.NoError(t, tier.Put(ctx, "g1", 1, meta))

	cases := []struct {
		name  string
		layer port.DisplayMetaLookup
	}{
		{
			name:  "L1 にも書き込まれている",
			layer: l1,
		},
		{
			name:  "L2 にも書き込まれている",
			layer: l2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.layer.Get(ctx, "g1", 1)
			require.NoError(t, err)
			require.Equal(t, meta, got)
		})
	}
}

// TestTwoTier_Put_RollsBackL1OnL2Failure は L2 書き込み失敗時に L1 を巻き戻し、
// error を呼び出し側に伝播することを検証する (L1 だけ snapshot を持つ非対称
// 状態 - pod ローカルでは見えるが他 pod / restart 後には消える状態 - を残さないため)。
func TestTwoTier_Put_RollsBackL1OnL2Failure(t *testing.T) {
	tier, l1, _, mr := newTestTwoTier(t)
	ctx := context.Background()
	mr.Close()

	err := tier.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7})
	require.Error(t, err)

	_, getErr := l1.Get(ctx, "g1", 1)
	require.ErrorIs(t, getErr, port.ErrNotFound)
}

// TestTwoTier_Get_ReturnsL1ValueWithoutTouchingL2 は L1 hit のとき L2 を参照せず
// L1 の値を返すことを検証する (L2 を停止しても影響しないことで「L2 read 抑制」を担保)。
func TestTwoTier_Get_ReturnsL1ValueWithoutTouchingL2(t *testing.T) {
	tier, l1, _, mr := newTestTwoTier(t)
	ctx := context.Background()
	meta := port.DisplayMeta{Name: "alice", Level: 7}
	require.NoError(t, l1.Put(ctx, "g1", 1, meta))
	mr.Close()

	got, err := tier.Get(ctx, "g1", 1)
	require.NoError(t, err)
	require.Equal(t, meta, got)
}

// TestTwoTier_Get_ReturnsL2ValueOnL2Hit は L1 miss / L2 hit のとき Get が L2 の
// 値を返すことを検証する。
func TestTwoTier_Get_ReturnsL2ValueOnL2Hit(t *testing.T) {
	tier, _, l2, _ := newTestTwoTier(t)
	ctx := context.Background()
	meta := port.DisplayMeta{Name: "alice", Level: 7}
	require.NoError(t, l2.Put(ctx, "g1", 1, meta))

	got, err := tier.Get(ctx, "g1", 1)
	require.NoError(t, err)
	require.Equal(t, meta, got)
}

// TestTwoTier_Get_PromotesL2HitToL1 は L1 miss / L2 hit のとき副作用として L1 に
// 値が昇格することを検証する (他 pod で seed された snapshot を自 pod の L1 にも
// 乗せて以降の Redis read を抑える設計)。
func TestTwoTier_Get_PromotesL2HitToL1(t *testing.T) {
	tier, l1, l2, _ := newTestTwoTier(t)
	ctx := context.Background()
	meta := port.DisplayMeta{Name: "alice", Level: 7}
	require.NoError(t, l2.Put(ctx, "g1", 1, meta))

	_, err := tier.Get(ctx, "g1", 1)
	require.NoError(t, err)

	gotL1, err := l1.Get(ctx, "g1", 1)
	require.NoError(t, err)
	require.Equal(t, meta, gotL1)
}

// TestTwoTier_Get_ReturnsNotFoundWhenBothLayersMiss は L1 / L2 いずれにも
// snapshot がないとき port.ErrNotFound を返すことを検証する (空文字 / level=0 等の
// silent fallback 禁止、呼び出し側のフォールバック判断にエラーを渡すため)。
func TestTwoTier_Get_ReturnsNotFoundWhenBothLayersMiss(t *testing.T) {
	tier, _, _, _ := newTestTwoTier(t)

	_, err := tier.Get(context.Background(), "g-missing", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestTwoTier_Get_PropagatesL1NonNotFoundError は L1 が ErrNotFound 以外の
// error を返したとき、L2 にフォールバックせずそのまま error を呼び出し側へ
// 伝播することを検証する (異常を握りつぶさず原因を観測可能に保つ)。
func TestTwoTier_Get_PropagatesL1NonNotFoundError(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l2 := NewRedisStore(client)
	tier := New(&stubL1{getErr: errors.New("l1 boom")}, l2)

	_, err := tier.Get(context.Background(), "g1", 1)
	require.Error(t, err)
	require.NotErrorIs(t, err, port.ErrNotFound)
}

// TestTwoTier_Get_PropagatesL1PromoteFailure は L2 hit 後の L1 昇格 (Put) が
// 失敗したとき、その error を呼び出し側へ伝播することを検証する (silent な
// 握りつぶしを避け、後続 PR の呼び出し側でフォールバック判断ができるようにする)。
func TestTwoTier_Get_PropagatesL1PromoteFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l2 := NewRedisStore(client)
	ctx := context.Background()
	require.NoError(t, l2.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
	tier := New(&stubL1{getErr: port.ErrNotFound, putErr: errors.New("l1 promote boom")}, l2)

	_, err := tier.Get(ctx, "g1", 1)
	require.Error(t, err)
	require.NotErrorIs(t, err, port.ErrNotFound)
}

// stubL1 は l1Tier interface を満たすテスト用 store。Get / Put が任意の
// error を返せる最小実装。
type stubL1 struct {
	getErr error
	putErr error
}

func (s *stubL1) Put(_ context.Context, _ string, _ int, _ port.DisplayMeta) error {
	return s.putErr
}

func (s *stubL1) Get(_ context.Context, _ string, _ int) (port.DisplayMeta, error) {
	return port.DisplayMeta{}, s.getErr
}

func (s *stubL1) Evict(_ context.Context, _ string, _ int) error {
	return nil
}
