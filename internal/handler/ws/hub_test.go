package ws

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	wsconst "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

type hubEvent struct {
	kind             string
	playerID, gameID string
	wasLate          bool
}

// hubCallbackRecorder は HubCallbacks の呼び出しを記録するテスト用実装である。
// GetGameID の戻り値は setInGame で制御する。
type hubCallbackRecorder struct {
	mu     sync.Mutex
	inGame map[string]string
	events []hubEvent
}

func newHubCallbackRecorder() *hubCallbackRecorder {
	return &hubCallbackRecorder{inGame: map[string]string{}}
}

func (r *hubCallbackRecorder) setInGame(playerID, gameID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inGame[playerID] = gameID
}

func (r *hubCallbackRecorder) record(e hubEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *hubCallbackRecorder) eventsOfKind(kind string) []hubEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []hubEvent
	for _, e := range r.events {
		if e.kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func (r *hubCallbackRecorder) callbacks() HubCallbacks {
	return HubCallbacks{
		GetGameID: func(playerID string) (string, bool) {
			r.mu.Lock()
			defer r.mu.Unlock()
			gid, ok := r.inGame[playerID]
			return gid, ok
		},
		OnDisconnectTimeout: func(playerID, gameID string) {
			r.record(hubEvent{kind: "disconnect_timeout", playerID: playerID, gameID: gameID})
		},
		OnMatchmakingLeave: func(playerID string) {
			r.record(hubEvent{kind: "matchmaking_leave", playerID: playerID})
		},
		OnGameDisconnect: func(playerID, gameID string) {
			r.record(hubEvent{kind: "game_disconnect", playerID: playerID, gameID: gameID})
		},
		OnGameReconnect: func(playerID, gameID string, wasLate bool) {
			r.record(hubEvent{kind: "game_reconnect", playerID: playerID, gameID: gameID, wasLate: wasLate})
		},
	}
}

func TestConnectionHubRegister(t *testing.T) {
	t.Run("[切断復帰]再接続時の切断状態引き継ぎ判定", func(t *testing.T) {
		t.Run("これまで一度も切断していないプレイヤーが接続するとき、対戦相手への復帰通知は行われない", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks()})
			factory := newTestSocketFactory(t, hub)

			factory.connect(t, "player-fresh")

			assert.Empty(t, recorder.eventsOfKind("game_reconnect"))
		})

		t.Run("切断してから猶予期間内に同じプレイヤーが再接続するとき(切断の記録がサーバー内に残っている間)、対戦相手へ「猶予期限内の復帰」として復帰が通知される", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), disconnectTimeout: time.Minute})
			factory := newTestSocketFactory(t, hub)
			_, conn1 := factory.connect(t, "player-1")
			hub.Unregister(conn1)

			factory.connect(t, "player-1")

			events := recorder.eventsOfKind("game_reconnect")
			require.Len(t, events, 1)
			assert.Equal(t, "player-1", events[0].playerID)
			assert.Equal(t, "game-1", events[0].gameID)
			assert.False(t, events[0].wasLate)
		})

		t.Run("切断の記録がサーバー内から失われている状態(プロセス再起動などをまたいだ場合)で、外部に引き継がれた切断記録の期限内に再接続するとき、対戦相手へ「猶予期限内の復帰」として復帰が通知される", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			recorder := newHubCallbackRecorder()
			timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
				return port.DisconnectDeadline{GameID: "game-external", Deadline: clock.Now().Add(time.Minute)}, true, nil
			}}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore, clock: clock})
			factory := newTestSocketFactory(t, hub)

			factory.connect(t, "player-external")

			events := recorder.eventsOfKind("game_reconnect")
			require.Len(t, events, 1)
			assert.Equal(t, "game-external", events[0].gameID)
			assert.False(t, events[0].wasLate)
		})

		t.Run("切断の記録がサーバー内から失われている状態で、外部に引き継がれた切断記録の期限を過ぎてから再接続するとき、対戦相手へ「猶予期限を過ぎた復帰」として復帰が通知される", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			recorder := newHubCallbackRecorder()
			timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
				return port.DisconnectDeadline{GameID: "game-external", Deadline: clock.Now().Add(-time.Minute)}, true, nil
			}}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore, clock: clock})
			factory := newTestSocketFactory(t, hub)

			factory.connect(t, "player-external")

			events := recorder.eventsOfKind("game_reconnect")
			require.Len(t, events, 1)
			assert.True(t, events[0].wasLate)
		})

		t.Run("切断の記録がサーバー内にも外部にも見当たらないとき、復帰は通知されない", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
				return port.DisconnectDeadline{}, false, nil
			}}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)

			factory.connect(t, "player-unknown")

			assert.Empty(t, recorder.eventsOfKind("game_reconnect"))
		})

		t.Run("外部の切断記録の読み出し自体が失敗したとき、復帰は通知されない", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
				return port.DisconnectDeadline{}, false, assert.AnError
			}}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)

			factory.connect(t, "player-broken-store")

			assert.Empty(t, recorder.eventsOfKind("game_reconnect"))
		})
	})

	t.Run("[接続管理]同一プレイヤーの二重接続と旧接続の後始末", func(t *testing.T) {
		t.Run("既に接続済みのプレイヤーが別のソケットで新たに接続するとき、古い方の接続は切断される", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-dup")

			factory.connect(t, "player-dup")

			client1.expectClosed(t)
		})

		t.Run("既に接続済みのプレイヤーが別のソケットで新たに接続したことで古い接続が切断されても、対戦相手への切断通知は行われず、再接続猶予タイマーも開始されず、マッチメイキングからの退出は発生しない(新しい接続への差し替えは切断として扱われない)", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-dup", "game-1")
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-dup")

			factory.connect(t, "player-dup")
			client1.expectClosed(t)

			assert.Empty(t, recorder.eventsOfKind("game_disconnect"))
			assert.Empty(t, recorder.eventsOfKind("matchmaking_leave"))
			assert.Empty(t, timerStore.setDisconnectDeadlineCallsSnapshot())
		})

		t.Run("新しい接続への差し替えの後、以後そのプレイヤー宛のメッセージ配信は新しい接続にのみ届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			factory := newTestSocketFactory(t, hub)
			factory.connect(t, "player-dup")
			client2, _ := factory.connect(t, "player-dup")

			hub.SendToPlayer("player-dup", &WSMessage{Type: "ping_test"})

			msg := client2.readMessage(t)
			assert.Equal(t, "ping_test", msg.Type)
		})
	})

	t.Run("[接続管理]接続登録", func(t *testing.T) {
		t.Run("接続が登録されると、外部に写された切断猶予期限の記録が削除される", func(t *testing.T) {
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)

			factory.connect(t, "player-1")

			assert.Contains(t, timerStore.clearDisconnectDeadlineCallsSnapshot(), "player-1")
		})

		t.Run("再接続してきたプレイヤーの切断猶予期限の写しの削除が失敗したとき、そのプレイヤー宛のメッセージは引き続きそのプレイヤーに届く", func(t *testing.T) {
			timerStore := &stubTimerStore{clearDisconnectDeadlineFunc: func(ctx context.Context, playerID string) error {
				return errors.New("clear failed")
			}}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")

			hub.SendToPlayer("player-1", &WSMessage{Type: "ping_test"})

			msg := client.readMessage(t)
			assert.Equal(t, "ping_test", msg.Type)
		})
	})
}

func TestConnectionHubUnregister(t *testing.T) {
	t.Run("[切断復帰]切断時の対戦所属有無による後始末の切り替え", func(t *testing.T) {
		t.Run("切断した接続が、その時点でそのプレイヤーに現在登録されている接続と一致しないとき(「同一プレイヤーの二重接続と旧接続の後始末」の規定による差し替えケース)、対戦相手への切断通知・再接続の猶予タイマー開始・マッチメイキングからの退出のいずれも発生しない", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)
			_, staleConn := factory.connect(t, "player-1")
			factory.connect(t, "player-1")

			hub.Unregister(staleConn)

			assert.Empty(t, recorder.eventsOfKind("game_disconnect"))
			assert.Empty(t, recorder.eventsOfKind("matchmaking_leave"))
			assert.Empty(t, timerStore.setDisconnectDeadlineCallsSnapshot())
		})

		t.Run("切断したプレイヤーが対戦中であるとき、対戦相手へ切断が通知される", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks()})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-1")

			hub.Unregister(conn)

			events := recorder.eventsOfKind("game_disconnect")
			require.Len(t, events, 1)
			assert.Equal(t, "player-1", events[0].playerID)
			assert.Equal(t, "game-1", events[0].gameID)
		})

		t.Run("切断したプレイヤーが対戦中であり、再接続の猶予タイマーの開始(切断猶予期限の書き込み)が失敗したときも、対戦相手へ切断が通知される", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			timerStore := &stubTimerStore{setDisconnectDeadlineFunc: func(ctx context.Context, playerID, gameID string, deadline time.Time) error {
				return errors.New("write failed")
			}}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-1")

			hub.Unregister(conn)

			events := recorder.eventsOfKind("game_disconnect")
			require.Len(t, events, 1)
			assert.Equal(t, "player-1", events[0].playerID)
			assert.Equal(t, "game-1", events[0].gameID)
		})

		t.Run("サーバーが一斉終了処理中に切断が起きたときは、切断通知だけが抑止される。猶予タイマー開始・マッチメイキング退出は通常どおり行われる", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			hub.Shutdown(context.Background())
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-1")

			hub.Unregister(conn)

			assert.Empty(t, recorder.eventsOfKind("game_disconnect"))
			assert.NotEmpty(t, timerStore.setDisconnectDeadlineCallsSnapshot())
			assert.NotEmpty(t, recorder.eventsOfKind("matchmaking_leave"))
		})

		t.Run("切断したプレイヤーが対戦中でないとき、対戦相手への通知は行われない(対戦相手が存在しないため)", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks()})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-not-in-game")

			hub.Unregister(conn)

			assert.Empty(t, recorder.eventsOfKind("game_disconnect"))
		})

		t.Run("切断したプレイヤーが対戦中であるとき、再接続の猶予タイマーが開始される", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-1")

			hub.Unregister(conn)

			calls := timerStore.setDisconnectDeadlineCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, "player-1", calls[0].playerID)
			assert.Equal(t, "game-1", calls[0].gameID)
		})

		t.Run("切断したプレイヤーが対戦中でないとき、猶予タイマーは開始されない", func(t *testing.T) {
			recorder := newHubCallbackRecorder()
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), timerStore: timerStore})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-not-in-game")

			hub.Unregister(conn)

			assert.Empty(t, timerStore.setDisconnectDeadlineCallsSnapshot())
		})

		t.Run("登録中の接続が切断されると、対戦中か否かに関わらず、マッチメイキングの待機列からの退出が行われる", func(t *testing.T) {
			t.Run("対戦中のとき", func(t *testing.T) {
				recorder := newHubCallbackRecorder()
				recorder.setInGame("player-1", "game-1")
				hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks()})
				factory := newTestSocketFactory(t, hub)
				_, conn := factory.connect(t, "player-1")

				hub.Unregister(conn)

				assert.Len(t, recorder.eventsOfKind("matchmaking_leave"), 1)
			})

			t.Run("対戦中でないとき", func(t *testing.T) {
				recorder := newHubCallbackRecorder()
				hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks()})
				factory := newTestSocketFactory(t, hub)
				_, conn := factory.connect(t, "player-1")

				hub.Unregister(conn)

				assert.Len(t, recorder.eventsOfKind("matchmaking_leave"), 1)
			})
		})
	})
}

func TestConnectionHubDisconnectTimeout(t *testing.T) {
	t.Run("[切断復帰]切断猶予期限超過時の対戦セッション側後始末の起動", func(t *testing.T) {
		t.Run("対戦中に切断し、猶予時間内に再接続されないとき、猶予時間の経過後に対戦セッション側の後始末処理(不戦敗が成立するかどうかを含め、「切断タイムアウトによる強制敗北」の規定に従う)が起動する", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), disconnectTimeout: 5 * time.Second, clock: clock})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-1")

			hub.Unregister(conn)
			clock.Advance(5 * time.Second)

			events := recorder.eventsOfKind("disconnect_timeout")
			require.Len(t, events, 1)
			assert.Equal(t, "player-1", events[0].playerID)
			assert.Equal(t, "game-1", events[0].gameID)
		})

		t.Run("対戦中に切断したが、猶予時間内に再接続するとき、猶予タイマーは停止され、対戦セッション側の後始末処理は起動しないまま対戦が続く", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), disconnectTimeout: 5 * time.Second, clock: clock})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-1")

			hub.Unregister(conn)
			clock.Advance(2 * time.Second)
			factory.connect(t, "player-1")
			clock.Advance(5 * time.Second)

			assert.Empty(t, recorder.eventsOfKind("disconnect_timeout"))
		})
	})
}

func TestConnectionHubIsDisconnectDeadlineExpired(t *testing.T) {
	t.Run("[切断復帰]切断猶予期限切れの判定", func(t *testing.T) {
		t.Run("同一プロセスが切断を検知し続けている間は、実際の経過時間に関わらず「期限切れではない」という結果が返る(インメモリのタイマーがまだ発火していないため)", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			recorder := newHubCallbackRecorder()
			recorder.setInGame("player-1", "game-1")
			hub := newTestHub(t, hubDeps{callbacks: recorder.callbacks(), disconnectTimeout: time.Hour, clock: clock})
			factory := newTestSocketFactory(t, hub)
			_, conn := factory.connect(t, "player-1")
			hub.Unregister(conn)

			expired, err := hub.IsDisconnectDeadlineExpired("player-1")

			require.NoError(t, err)
			assert.False(t, expired)
		})

		t.Run("プロセス再起動などをまたいで切断の記録が外部に引き継がれている場合、記録された期限と現在時刻を比較した結果(期限切れかどうか)が返る", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			cases := []struct {
				name            string
				deadlineFromNow time.Duration
				wantExpired     bool
			}{
				{"記録された期限がまだ先のとき", time.Minute, false},
				{"記録された期限を過ぎているとき", -time.Minute, true},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
						return port.DisconnectDeadline{GameID: "game-1", Deadline: clock.Now().Add(tt.deadlineFromNow)}, true, nil
					}}
					hub := newTestHub(t, hubDeps{timerStore: timerStore, clock: clock})

					expired, err := hub.IsDisconnectDeadlineExpired("player-external")

					require.NoError(t, err)
					assert.Equal(t, tt.wantExpired, expired)
				})
			}
		})

		t.Run("外部に切断の記録が見当たらないとき、「期限切れではない」という結果が返る", func(t *testing.T) {
			timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
				return port.DisconnectDeadline{}, false, nil
			}}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})

			expired, err := hub.IsDisconnectDeadlineExpired("player-unknown")

			require.NoError(t, err)
			assert.False(t, expired)
		})

		t.Run("外部の記録の読み出し自体が失敗したとき、呼び出し元にエラーが返る", func(t *testing.T) {
			timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
				return port.DisconnectDeadline{}, false, assert.AnError
			}}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})

			_, err := hub.IsDisconnectDeadlineExpired("player-broken-store")

			assert.Error(t, err)
		})
	})
}

func TestConnectionHubShutdown(t *testing.T) {
	t.Run("[接続管理]シャットダウン時の一斉接続終了", func(t *testing.T) {
		t.Run("登録されている接続が無い(0件)とき、通知対象が無いため即座に処理が呼び出し元に戻る", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})

			start := time.Now()
			hub.Shutdown(context.Background())

			assert.Less(t, time.Since(start), 500*time.Millisecond)
		})

		t.Run("登録されている接続が1件のとき、その接続にサーバー更新の通知が送られ、接続が閉じられる", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")

			hub.Shutdown(context.Background())

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgServerUpdate, msg.Type)
			client.expectClosed(t)
		})

		t.Run("登録されている接続が複数件のとき、全ての接続にサーバー更新の通知が送られ、それぞれ閉じられる", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")

			hub.Shutdown(context.Background())

			assert.Equal(t, wsconst.WSServerMsgServerUpdate, client1.readMessage(t).Type)
			assert.Equal(t, wsconst.WSServerMsgServerUpdate, client2.readMessage(t).Type)
			client1.expectClosed(t)
			client2.expectClosed(t)
		})
	})
}

// delayedShutdownNotifier は Shutdown 呼び出しの完了を指定時間だけ遅らせる shutdownNotifier
// のテスト用実装である。completed は Shutdown が実際に最後まで実行されたかを表す。
type delayedShutdownNotifier struct {
	delay     time.Duration
	completed atomic.Bool
}

func (n *delayedShutdownNotifier) Shutdown(code int, reason string) {
	time.Sleep(n.delay)
	n.completed.Store(true)
}

func TestShutdownAll(t *testing.T) {
	t.Run("[接続管理]一斉終了通知の上限時間による打ち切り", func(t *testing.T) {
		t.Run("全ての通知が上限時間内に完了するとき、全ての通知の完了を待ってから処理が呼び出し元に戻る", func(t *testing.T) {
			n1 := &delayedShutdownNotifier{delay: 10 * time.Millisecond}
			n2 := &delayedShutdownNotifier{delay: 10 * time.Millisecond}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			shutdownAll(ctx, []shutdownNotifier{n1, n2}, websocket.CloseGoingAway, wsconst.WSServerMsgServerUpdate)

			assert.True(t, n1.completed.Load())
			assert.True(t, n2.completed.Load())
		})

		t.Run("通知が上限時間内に完了しないとき、その完了を待たずに処理が呼び出し元に戻る", func(t *testing.T) {
			n := &delayedShutdownNotifier{delay: 300 * time.Millisecond}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()

			shutdownAll(ctx, []shutdownNotifier{n}, websocket.CloseGoingAway, wsconst.WSServerMsgServerUpdate)

			assert.False(t, n.completed.Load())
		})
	})
}
