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
		return NewConnectionHub(cb, DefaultDisconnectTimeout, nil)
	}
	return NewConnectionHub(cb, DefaultDisconnectTimeout, timerStore)
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
			assert.WithinDuration(t, before.Add(120*time.Second), calls[0].deadline, 2*time.Second)
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

// reconnectCall は OnGameReconnect コールバックへの 1 回の呼出を記録する。
type reconnectCall struct {
	playerID string
	gameID   string
	wasLate  bool
}

func TestRegister_WasLate(t *testing.T) {
	t.Run("復帰したプレイヤー自身の猶予切れ判定", func(t *testing.T) {
		t.Run("インメモリのタイマーがまだ残っているとき、猶予切れではないと判定される", func(t *testing.T) {
			var calls []reconnectCall
			hub := NewConnectionHub(HubCallbacks{
				GetGameID:           func(string) (string, bool) { return "game_1", true },
				OnDisconnectTimeout: func(string, string) {},
				OnGameReconnect: func(playerID, gameID string, wasLate bool) {
					calls = append(calls, reconnectCall{playerID, gameID, wasLate})
				},
			}, DefaultDisconnectTimeout, nil)
			conn := NewConnection(nil, "p1")
			hub.Register(conn)
			hub.Unregister(conn)

			hub.Register(NewConnection(nil, "p1"))

			require.Len(t, calls, 1)
			assert.Equal(t, "game_1", calls[0].gameID)
			assert.False(t, calls[0].wasLate)
		})

		t.Run("インメモリに記録が無く写しの期限がまだ先のとき、猶予切れではないと判定される", func(t *testing.T) {
			var calls []reconnectCall
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("game_1", time.Now().Add(time.Minute)),
			}
			hub := NewConnectionHub(HubCallbacks{
				GetGameID:           func(string) (string, bool) { return "", false },
				OnDisconnectTimeout: func(string, string) {},
				OnGameReconnect: func(playerID, gameID string, wasLate bool) {
					calls = append(calls, reconnectCall{playerID, gameID, wasLate})
				},
			}, DefaultDisconnectTimeout, store)

			hub.Register(NewConnection(nil, "p1"))

			require.Len(t, calls, 1)
			assert.Equal(t, "game_1", calls[0].gameID)
			assert.False(t, calls[0].wasLate)
		})

		t.Run("インメモリに記録が無く写しの期限が過ぎているとき、猶予切れとして判定される", func(t *testing.T) {
			var calls []reconnectCall
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("game_1", time.Now().Add(-time.Minute)),
			}
			hub := NewConnectionHub(HubCallbacks{
				GetGameID:           func(string) (string, bool) { return "", false },
				OnDisconnectTimeout: func(string, string) {},
				OnGameReconnect: func(playerID, gameID string, wasLate bool) {
					calls = append(calls, reconnectCall{playerID, gameID, wasLate})
				},
			}, DefaultDisconnectTimeout, store)

			hub.Register(NewConnection(nil, "p1"))

			require.Len(t, calls, 1)
			assert.Equal(t, "game_1", calls[0].gameID)
			assert.True(t, calls[0].wasLate)
		})

		t.Run("インメモリにも写しにも記録が無いとき、復帰処理を実行しない", func(t *testing.T) {
			var calls []reconnectCall
			store := &fakeTimerStore{}
			hub := NewConnectionHub(HubCallbacks{
				GetGameID:           func(string) (string, bool) { return "", false },
				OnDisconnectTimeout: func(string, string) {},
				OnGameReconnect: func(playerID, gameID string, wasLate bool) {
					calls = append(calls, reconnectCall{playerID, gameID, wasLate})
				},
			}, DefaultDisconnectTimeout, store)

			hub.Register(NewConnection(nil, "p1"))

			assert.Empty(t, calls)
		})

		t.Run("インメモリに記録が無く写しの読み出しが失敗するとき、復帰処理を実行しない", func(t *testing.T) {
			var calls []reconnectCall
			store := &fakeTimerStore{getDisconnectErr: errors.New("redis down")}
			hub := NewConnectionHub(HubCallbacks{
				GetGameID:           func(string) (string, bool) { return "", false },
				OnDisconnectTimeout: func(string, string) {},
				OnGameReconnect: func(playerID, gameID string, wasLate bool) {
					calls = append(calls, reconnectCall{playerID, gameID, wasLate})
				},
			}, DefaultDisconnectTimeout, store)

			hub.Register(NewConnection(nil, "p1"))

			assert.Empty(t, calls, "must not resolve stale disconnect when the mirror read fails")
		})
	})
}

func TestIsConnected(t *testing.T) {
	t.Run("接続状況の判定", func(t *testing.T) {
		t.Run("Register済みのプレイヤーはtrueになる", func(t *testing.T) {
			hub := newTestHub(nil, false, "")
			hub.Register(NewConnection(nil, "p1"))

			assert.True(t, hub.IsConnected("p1"))
		})

		t.Run("Unregister後はfalseになる", func(t *testing.T) {
			hub := newTestHub(nil, false, "")
			conn := NewConnection(nil, "p1")
			hub.Register(conn)

			hub.Unregister(conn)

			assert.False(t, hub.IsConnected("p1"))
		})

		t.Run("一度も接続していないプレイヤーはfalseになる", func(t *testing.T) {
			hub := newTestHub(nil, false, "")

			assert.False(t, hub.IsConnected("unknown"))
		})
	})
}

func TestIsDisconnectDeadlineExpired(t *testing.T) {
	t.Run("切断猶予期限の期限切れ判定", func(t *testing.T) {
		t.Run("インメモリのタイマーがまだ残っているとき、falseになる", func(t *testing.T) {
			hub := newTestHub(nil, true, "game_1")
			conn := NewConnection(nil, "p1")
			hub.Register(conn)
			hub.Unregister(conn)

			expired, err := hub.IsDisconnectDeadlineExpired("p1")

			require.NoError(t, err)
			assert.False(t, expired)
		})

		t.Run("インメモリにも写しにも記録が無いとき、falseになる", func(t *testing.T) {
			hub := newTestHub(&fakeTimerStore{}, true, "game_1")

			expired, err := hub.IsDisconnectDeadlineExpired("p1")

			require.NoError(t, err)
			assert.False(t, expired)
		})

		t.Run("インメモリに記録が無く写しの期限がまだ先のとき、falseになる", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("game_1", time.Now().Add(time.Minute)),
			}
			hub := newTestHub(store, true, "game_1")

			expired, err := hub.IsDisconnectDeadlineExpired("p1")

			require.NoError(t, err)
			assert.False(t, expired)
		})

		t.Run("インメモリに記録が無く写しの期限が過ぎているとき、trueになる", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("game_1", time.Now().Add(-time.Minute)),
			}
			hub := newTestHub(store, true, "game_1")

			expired, err := hub.IsDisconnectDeadlineExpired("p1")

			require.NoError(t, err)
			assert.True(t, expired)
		})

		t.Run("インメモリに記録が無く写しの期限にちょうど達しているとき、trueになる", func(t *testing.T) {
			now := time.Now()
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("game_1", now),
			}
			hub := newTestHub(store, true, "game_1")

			expired, err := hub.IsDisconnectDeadlineExpired("p1")

			require.NoError(t, err)
			assert.True(t, expired, "a deadline equal to the check time counts as expired")
		})

		t.Run("インメモリに記録が無く写しの期限に達する直前のとき、falseになる", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("game_1", time.Now().Add(50*time.Millisecond)),
			}
			hub := newTestHub(store, true, "game_1")

			expired, err := hub.IsDisconnectDeadlineExpired("p1")

			require.NoError(t, err)
			assert.False(t, expired)
		})

		t.Run("インメモリに記録が無く写しの期限を過ぎた直後のとき、trueになる", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("game_1", time.Now().Add(-50*time.Millisecond)),
			}
			hub := newTestHub(store, true, "game_1")

			expired, err := hub.IsDisconnectDeadlineExpired("p1")

			require.NoError(t, err)
			assert.True(t, expired)
		})

		t.Run("インメモリに記録が無く写しの読み出しに失敗するとき、エラーを返す", func(t *testing.T) {
			store := &fakeTimerStore{getDisconnectErr: errors.New("redis down")}
			hub := newTestHub(store, true, "game_1")

			_, err := hub.IsDisconnectDeadlineExpired("p1")

			assert.Error(t, err)
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
