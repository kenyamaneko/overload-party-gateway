package ws

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// shutdownCall は fakeShutdownNotifier.Shutdown への 1 回の呼出を記録する。
type shutdownCall struct {
	code   int
	reason string
}

// gameDisconnectCall は HubCallbacks.OnGameDisconnect への 1 回の呼出を記録する。
type gameDisconnectCall struct {
	playerID string
	gameID   string
}

// fakeShutdownNotifier は shutdownAll の待ち合わせ挙動をネットワークを介さず検証するための
// テスト用 shutdownNotifier 実装。
type fakeShutdownNotifier struct {
	calls      *int32
	blockUntil <-chan struct{} // nil なら即座に完了する

	mu            sync.Mutex
	shutdownCalls []shutdownCall
}

func (f *fakeShutdownNotifier) Shutdown(code int, reason string) {
	if f.calls != nil {
		atomic.AddInt32(f.calls, 1)
	}
	f.mu.Lock()
	f.shutdownCalls = append(f.shutdownCalls, shutdownCall{code, reason})
	f.mu.Unlock()
	if f.blockUntil != nil {
		<-f.blockUntil
	}
}

func (f *fakeShutdownNotifier) snapshotShutdownCalls() []shutdownCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]shutdownCall, len(f.shutdownCalls))
	copy(out, f.shutdownCalls)
	return out
}

func TestShutdownAll(t *testing.T) {
	t.Run("[停止時の対戦保護]一括シャットダウンの待ち合わせ", func(t *testing.T) {
		t.Run("全ての対象が期限内に終了通知を終えるとき、全対象へ終了通知が行われる", func(t *testing.T) {
			n1 := &fakeShutdownNotifier{}
			n2 := &fakeShutdownNotifier{}
			targets := []shutdownNotifier{n1, n2}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			shutdownAll(ctx, targets, websocket.CloseGoingAway, genws.WSServerMsgServerUpdate)

			for _, n := range []*fakeShutdownNotifier{n1, n2} {
				got := n.snapshotShutdownCalls()
				require.Len(t, got, 1)
				assert.Equal(t, websocket.CloseGoingAway, got[0].code)
				assert.Equal(t, genws.WSServerMsgServerUpdate, got[0].reason)
			}
		})

		t.Run("一部の対象が期限内に終了通知を終えないとき、期限に達した時点で待たずに返る", func(t *testing.T) {
			stuck := make(chan struct{})
			t.Cleanup(func() { close(stuck) })

			var calls int32
			targets := []shutdownNotifier{
				&fakeShutdownNotifier{calls: &calls},
				&fakeShutdownNotifier{calls: &calls, blockUntil: stuck},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			start := time.Now()
			shutdownAll(ctx, targets, websocket.CloseGoingAway, genws.WSServerMsgServerUpdate)
			elapsed := time.Since(start)

			assert.Less(t, elapsed, 2*time.Second,
				"完了しない対象を待たずに ctx の期限で返るはずが、待ち続けている")
		})

		t.Run("通知対象が無いとき、期限を待たずに返る", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			start := time.Now()
			shutdownAll(ctx, nil, websocket.CloseGoingAway, genws.WSServerMsgServerUpdate)
			elapsed := time.Since(start)

			assert.Less(t, elapsed, 500*time.Millisecond,
				"通知対象が無いのに、ctx の期限近くまで待ってから返っている")
		})
	})
}

func TestConnectionHubShutdown(t *testing.T) {
	t.Run("[停止時の対戦保護]全接続への一斉終了通知", func(t *testing.T) {
		t.Run("登録されている接続はすべて、シャットダウン用のcloseコードと終了理由を持って閉じられる", func(t *testing.T) {
			hub := NewConnectionHub(HubCallbacks{
				GetGameID: func(string) (string, bool) { return "", false },
			}, DefaultDisconnectTimeout, nil)
			conn1 := NewConnection(nil, "p1")
			conn2 := NewConnection(nil, "p2")
			hub.Register(conn1)
			hub.Register(conn2)

			// WritePump を起動していない接続は close フレームを書き終えられないため、
			// Shutdown は ctx の期限まで戻らない。短い期限で確認する。
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			hub.Shutdown(ctx)

			for _, c := range []*Connection{conn1, conn2} {
				c.mu.Lock()
				isClosed, code, reason := c.isClosed, c.closeCode, c.closeReason
				c.mu.Unlock()
				assert.True(t, isClosed, "connection should be marked closed after hub shutdown")
				assert.Equal(t, websocket.CloseGoingAway, code)
				assert.Equal(t, genws.WSServerMsgServerUpdate, reason)
			}
		})

		t.Run("対戦相手への切断通知の抑止", func(t *testing.T) {
			t.Run("シャットダウン中の切断のとき、相手への切断通知を送らない", func(t *testing.T) {
				var notifyCalls int32
				hub := NewConnectionHub(HubCallbacks{
					GetGameID:           func(string) (string, bool) { return "g1", true },
					OnDisconnectTimeout: func(string, string) {},
					OnGameDisconnect:    func(string, string) { atomic.AddInt32(&notifyCalls, 1) },
				}, DefaultDisconnectTimeout, nil)
				conn := NewConnection(nil, "p1")
				hub.Register(conn)

				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				hub.Shutdown(ctx)
				hub.Unregister(conn)

				assert.EqualValues(t, 0, atomic.LoadInt32(&notifyCalls))
			})

			t.Run("シャットダウンしていない切断のとき、相手への切断通知を送る", func(t *testing.T) {
				var notifyCalls []gameDisconnectCall
				hub := NewConnectionHub(HubCallbacks{
					GetGameID:           func(string) (string, bool) { return "g1", true },
					OnDisconnectTimeout: func(string, string) {},
					OnGameDisconnect: func(playerID, gameID string) {
						notifyCalls = append(notifyCalls, gameDisconnectCall{playerID, gameID})
					},
				}, DefaultDisconnectTimeout, nil)
				conn := NewConnection(nil, "p1")
				hub.Register(conn)

				hub.Unregister(conn)

				require.Len(t, notifyCalls, 1)
				assert.Equal(t, "p1", notifyCalls[0].playerID)
				assert.Equal(t, "g1", notifyCalls[0].gameID)
			})
		})
	})
}
