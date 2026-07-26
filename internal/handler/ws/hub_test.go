package ws

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHub は timerStore を差し替え可能な ConnectionHub を返す。timerStore が
// nil (*fakeTimerStore の型付き nil) のときは、port.TimerStore が非 nil インター
// フェースにならないよう明示的に untyped nil を渡す。
// OnDisconnectTimeout は即時発火を避けるため noop にし、GetGameID は inGame の
// 応答を切り替えられるようにする。
func newTestHub(timerStore *fakeTimerStore, inGame bool, gameID string) *ConnectionHub {
	cb := HubCallbacks{
		GetGameID:           func(string) (string, bool) { return gameID, inGame },
		OnDisconnectTimeout: func(string, string) {},
	}
	if timerStore == nil {
		return NewConnectionHub(cb, nil)
	}
	return NewConnectionHub(cb, timerStore)
}

func TestUnregister(t *testing.T) {
	t.Run("切断時の写しの書き込み", func(t *testing.T) {
		t.Run("ゲームに参加中のプレイヤーが切断すると、猶予期限が書き込まれる", func(t *testing.T) {
			store := &fakeTimerStore{}
			hub := newTestHub(store, true, "game_1")
			conn := NewConnection(nil, "p1")
			hub.Register(conn)
			before := time.Now()

			hub.Unregister(conn)

			calls := store.snapshotSetDisconnectCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "p1", calls[0].playerID)
			assert.Equal(t, "game_1", calls[0].gameID)
			assert.WithinDuration(t, before.Add(60*time.Second), calls[0].deadline, 2*time.Second)
		})

		t.Run("ゲームに参加していないプレイヤーが切断すると、猶予期限を書き込まない", func(t *testing.T) {
			store := &fakeTimerStore{}
			hub := newTestHub(store, false, "")
			conn := NewConnection(nil, "p1")
			hub.Register(conn)

			hub.Unregister(conn)

			assert.Empty(t, store.snapshotSetDisconnectCalls())
		})

		t.Run("写しが未設定のとき、パニックしない", func(t *testing.T) {
			hub := newTestHub(nil, true, "game_1")
			conn := NewConnection(nil, "p1")
			hub.Register(conn)

			assert.NotPanics(t, func() { hub.Unregister(conn) })
		})

		t.Run("写しの書き込みが失敗しても、パニックしない", func(t *testing.T) {
			store := &fakeTimerStore{setDisconnectErr: errors.New("redis down")}
			hub := newTestHub(store, true, "game_1")
			conn := NewConnection(nil, "p1")
			hub.Register(conn)

			assert.NotPanics(t, func() { hub.Unregister(conn) })
		})

		t.Run("既に解除済みの接続を再度切断しても、書き込まない", func(t *testing.T) {
			store := &fakeTimerStore{}
			hub := newTestHub(store, true, "game_1")
			conn := NewConnection(nil, "p1")
			hub.Register(conn)
			hub.Unregister(conn)

			hub.Unregister(conn)

			assert.Len(t, store.snapshotSetDisconnectCalls(), 1, "second Unregister on the same conn must be a no-op")
		})
	})
}

func TestRegister(t *testing.T) {
	t.Run("再接続時の写しの削除", func(t *testing.T) {
		t.Run("登録すると、そのプレイヤーの猶予期限が削除される", func(t *testing.T) {
			store := &fakeTimerStore{}
			hub := newTestHub(store, true, "game_1")
			conn := NewConnection(nil, "p1")

			hub.Register(conn)

			calls := store.snapshotClearDisconnectCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "p1", calls[0])
		})

		t.Run("写しが未設定のとき、パニックしない", func(t *testing.T) {
			hub := newTestHub(nil, true, "game_1")
			conn := NewConnection(nil, "p1")

			assert.NotPanics(t, func() { hub.Register(conn) })
		})

		t.Run("写しからの削除が失敗しても、パニックしない", func(t *testing.T) {
			store := &fakeTimerStore{clearDisconnectErr: errors.New("redis down")}
			hub := newTestHub(store, true, "game_1")
			conn := NewConnection(nil, "p1")

			assert.NotPanics(t, func() { hub.Register(conn) })
		})
	})
}

func TestClearDisconnectDeadline(t *testing.T) {
	t.Run("ゲーム終了時などの明示的な猶予期限削除", func(t *testing.T) {
		t.Run("呼ぶと、そのプレイヤーの猶予期限が削除される", func(t *testing.T) {
			store := &fakeTimerStore{}
			hub := newTestHub(store, true, "game_1")

			hub.ClearDisconnectDeadline("p1")

			assert.Equal(t, []string{"p1"}, store.snapshotClearDisconnectCalls())
		})

		t.Run("写しが未設定のとき、パニックしない", func(t *testing.T) {
			hub := newTestHub(nil, true, "game_1")

			assert.NotPanics(t, func() { hub.ClearDisconnectDeadline("p1") })
		})
	})
}
