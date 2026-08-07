package ws

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// syncBuffer は別 goroutine からのログ書き込みと読み出しを直列化する記録先。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// logRecord は捕捉したログ 1 件のうち検証に使うフィールド。
type logRecord struct {
	Level    string `json:"level"`
	GameID   string `json:"game_id"`
	PlayerID string `json:"player_id"`
	Error    string `json:"error"`
}

// captureLogs は既定のログ出力先をテスト内の記録先に差し替え、捕捉したログを読み出す関数を返す。
func captureLogs(t *testing.T) func() []logRecord {
	t.Helper()
	buf := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return func() []logRecord {
		var records []logRecord
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var rec logRecord
			require.NoError(t, json.Unmarshal([]byte(line), &rec))
			records = append(records, rec)
		}
		return records
	}
}

// findErrorLogs は gameID と playerID を伴うエラーログだけを抜き出す。
func findErrorLogs(records []logRecord, gameID, playerID string) []logRecord {
	var found []logRecord
	for _, rec := range records {
		if rec.Level == "ERROR" && rec.GameID == gameID && rec.PlayerID == playerID {
			found = append(found, rec)
		}
	}
	return found
}

// findWarnLogs は playerID を伴う警告ログだけを抜き出す。
func findWarnLogs(records []logRecord, playerID string) []logRecord {
	var found []logRecord
	for _, rec := range records {
		if rec.Level == "WARN" && rec.PlayerID == playerID {
			found = append(found, rec)
		}
	}
	return found
}

// findSentMessage は接続の送信バッファから wantType のメッセージを取り出す。
func findSentMessage(t *testing.T, conn *Connection, wantType string) WSMessage {
	t.Helper()
	for {
		select {
		case data := <-conn.send:
			var msg WSMessage
			require.NoError(t, json.Unmarshal(data, &msg))
			if msg.Type == wantType {
				return msg
			}
		default:
			require.FailNowf(t, "expected message was not sent",
				"no %q message was sent to %s", wantType, conn.playerID)
			return WSMessage{}
		}
	}
}

// newDisconnectResolutionRelay は切断・再接続時の決着ロジックを検証するための GameRelay を返す。
// 実際の ConnectionHub を使うことで hub.IsConnected / hub.IsDisconnectDeadlineExpired が
// テストごとの接続状況を正しく反映する。
func newDisconnectResolutionRelay(entries []port.GamePlayerEntry, timerStore *fakeTimerStore) (*GameRelay, *mockBattleClient, *ConnectionHub) {
	bc := newMockBattleClient()
	var store port.TimerStore
	if timerStore != nil {
		store = timerStore
	}
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	}, DefaultDisconnectTimeout, store)
	repo := &mockGamePlayerRepo{lookupEntries: entries}
	relay := NewGameRelay(hub, bc, nil, repo, newFakeInvalidatedGameRepo())
	return relay, bc, hub
}

func TestOpponentPlayerID(t *testing.T) {
	t.Run("[切断・再接続]対戦相手のプレイヤーID解決", func(t *testing.T) {
		tests := []struct {
			name    string
			entries []port.GamePlayerEntry
			selfID  string
			want    string
		}{
			{
				name: "2人制で自分が1Pのとき、2PのプレイヤーIDを返す",
				entries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
					{PlayerNum: 2, PlayerID: "p2"},
				},
				selfID: "p1",
				want:   "p2",
			},
			{
				name: "2人制で自分が2Pのとき、1PのプレイヤーIDを返す",
				entries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
					{PlayerNum: 2, PlayerID: "p2"},
				},
				selfID: "p2",
				want:   "p1",
			},
			{
				name: "エントリが1件 (NPC戦)のとき、空文字を返す",
				entries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
				},
				selfID: "p1",
				want:   "",
			},
			{
				name:    "エントリが0件のとき、空文字を返す",
				entries: nil,
				selfID:  "p1",
				want:    "",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, opponentPlayerID(tt.entries, tt.selfID))
			})
		}
	})
}

func TestPlayerNumOf(t *testing.T) {
	entries := []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1"},
		{PlayerNum: 2, PlayerID: "p2"},
	}

	t.Run("[切断・再接続]プレイヤー番号の解決", func(t *testing.T) {
		tests := []struct {
			name     string
			playerID string
			want     int
		}{
			{name: "1Pとして参加しているとき、1を返す", playerID: "p1", want: 1},
			{name: "2Pとして参加しているとき、2を返す", playerID: "p2", want: 2},
			{name: "参加していないとき、0を返す", playerID: "p3", want: 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, playerNumOf(entries, tt.playerID))
			})
		}
	})
}

func TestHandleDisconnectTimeout(t *testing.T) {
	entries := []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1"},
		{PlayerNum: 2, PlayerID: "p2"},
	}

	t.Run("[切断・再接続]切断猶予切れの決着判定", func(t *testing.T) {
		t.Run("対戦相手が接続中のとき、forfeitを実行する", func(t *testing.T) {
			relay, bc, hub := newDisconnectResolutionRelay(entries, nil)
			relay.JoinGame("p1", "g1", 1)
			hub.Register(NewConnection(nil, "p2"))
			bc.processActionResult = &service.ActionResult{}

			relay.HandleDisconnectTimeout("p1", "g1")

			require.Len(t, bc.processActionCalls, 1)
			assert.Equal(t, "g1", bc.processActionCalls[0].gameID)
			assert.Equal(t, 1, bc.processActionCalls[0].playerNum)
			assert.Equal(t, gamelogic.ActionTypeForfeit, bc.processActionCalls[0].actionType)
		})

		t.Run("対戦相手も切断中のとき、切断タイムアウトでもターンタイマー期限切れでもforfeitを実行しない", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay(entries, nil)
			relay.JoinGame("p1", "g1", 1)
			relay.resetTurnTimer("g1", "p1", 1) // timeBankSeconds=1 (+turnTimerNetworkBuffer) で数秒後に自然発火する

			relay.HandleDisconnectTimeout("p1", "g1")

			assert.Never(t, func() bool {
				return len(bc.snapshotProcessActionCalls()) > 0
			}, 5*time.Second, 100*time.Millisecond,
				"must not forfeit while the opponent is also disconnected, even after the turn timer later fires")
		})

		t.Run("NPC戦 (対戦相手が居ない)のとき、forfeitを実行する", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay([]port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "p1"}}, nil)
			relay.JoinGame("p1", "g1", 1)
			bc.processActionResult = &service.ActionResult{}

			relay.HandleDisconnectTimeout("p1", "g1")

			require.Len(t, bc.processActionCalls, 1)
		})

		t.Run("ゲーム参加者情報の取得に失敗するとき、forfeitを実行しない", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay(nil, nil)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupErr: errors.New("db down")}
			relay.JoinGame("p1", "g1", 1)

			relay.HandleDisconnectTimeout("p1", "g1")

			assert.Empty(t, bc.processActionCalls)
		})

		t.Run("入室していないプレイヤーの猶予が切れたとき、ゲームの参加者情報のスロット番号でforfeitを実行する", func(t *testing.T) {
			relay, bc, hub := newDisconnectResolutionRelay(entries, nil)
			hub.Register(NewConnection(nil, "p1"))
			bc.processActionResult = &service.ActionResult{}

			relay.HandleDisconnectTimeout("p2", "g1")

			require.Len(t, bc.processActionCalls, 1)
			assert.Equal(t, "g1", bc.processActionCalls[0].gameID)
			assert.Equal(t, 2, bc.processActionCalls[0].playerNum)
			assert.Equal(t, gamelogic.ActionTypeForfeit, bc.processActionCalls[0].actionType)
		})

		t.Run("ゲームの参加者として登録されていないプレイヤーの猶予が切れたとき、forfeitを実行せずエラーログに残す", func(t *testing.T) {
			readLogs := captureLogs(t)
			relay, bc, _ := newDisconnectResolutionRelay(nil, nil)

			relay.HandleDisconnectTimeout("p9", "g1")

			assert.Empty(t, bc.processActionCalls)
			logged := findErrorLogs(readLogs(), "g1", "p9")
			require.Len(t, logged, 1)
			assert.Equal(t, "player has no slot in the game", logged[0].Error)
		})
	})
}

func TestResolveStaleDisconnect(t *testing.T) {
	entries := []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1"},
		{PlayerNum: 2, PlayerID: "p2"},
	}

	t.Run("[切断・再接続]復帰時点での両者の猶予評価", func(t *testing.T) {
		t.Run("対戦相手が接続中のとき、何もしない", func(t *testing.T) {
			relay, bc, hub := newDisconnectResolutionRelay(entries, nil)
			hub.Register(NewConnection(nil, "p2"))

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})

		t.Run("対戦相手が切断中だが猶予内のとき、何もしない", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(time.Minute)),
			}
			relay, bc, _ := newDisconnectResolutionRelay(entries, store)

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})

		t.Run("対戦相手の切断猶予期限のバックアップの読み出しに失敗するとき、エラーログを残しつつforfeitを実行しない", func(t *testing.T) {
			readLogs := captureLogs(t)
			store := &fakeTimerStore{getDisconnectErr: errors.New("redis down")}
			relay, bc, _ := newDisconnectResolutionRelay(entries, store)

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls, "opponent's true disconnect state is unknown, so the game must stay unresolved rather than risk an incorrect forfeit")
			errorLogs := findErrorLogs(readLogs(), "g1", "")
			require.Len(t, errorLogs, 1)
			assert.Equal(t, "redis down", errorLogs[0].Error)
		})

		t.Run("対戦相手の猶予が切れており復帰した本人は猶予内だったとき、対戦相手のforfeitを実行する", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, hub := newDisconnectResolutionRelay(entries, store)
			relay.JoinGame("p2", "g1", 2)
			hub.Register(NewConnection(nil, "p1")) // 復帰した本人は接続済み
			bc.processActionResult = &service.ActionResult{}

			relay.resolveStaleDisconnect("g1", "p1", false)

			require.Len(t, bc.processActionCalls, 1)
			assert.Equal(t, 2, bc.processActionCalls[0].playerNum, "forfeit must be attributed to the still-disconnected opponent")
			assert.Equal(t, gamelogic.ActionTypeForfeit, bc.processActionCalls[0].actionType)
		})

		t.Run("対戦相手が入室しておらず猶予も切れているとき、復帰した側の勝ちでgame_overが届く", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, hub := newDisconnectResolutionRelay(entries, store)
			returning := NewConnection(nil, "p1")
			hub.Register(returning)
			relay.JoinGame("p1", "g1", 1)
			bc.processActionResult = &service.ActionResult{
				GameOver:         true,
				WinningPlayerNum: 1,
				WinReason:        gamelogic.WinReasonDisconnect,
			}

			relay.resolveStaleDisconnect("g1", "p1", false)

			calls := bc.snapshotProcessActionCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, 2, calls[0].playerNum, "forfeit must be attributed to the still-disconnected opponent")
			assert.Equal(t, gamelogic.ActionTypeForfeit, calls[0].actionType)

			gameOver := findSentMessage(t, returning, genws.WSServerMsgGameOver)
			var over GameOverMessage
			require.NoError(t, json.Unmarshal(gameOver.Data, &over))
			assert.EqualValues(t, 1, over.WinningPlayerNum)
			assert.Equal(t, gamelogic.WinReasonDisconnect, over.WinReason)
		})

		t.Run("両者とも猶予が切れているとき、両者強制決着で対戦を終え勝者なしでEXP付与を依頼する", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, hub := newDisconnectResolutionRelay(entries, store)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupEntries: entries, markAwardedReturn: true}
			account := &awardCounter{}
			srv := httptest.NewServer(account.handler())
			t.Cleanup(srv.Close)
			relay.accountClient = accountclient.New(srv.URL, &http.Client{})
			relay.JoinGame("p1", "g1", 1)
			relay.JoinGame("p2", "g1", 2)
			hub.Register(NewConnection(nil, "p1")) // 復帰した本人は接続済み
			bc.processActionResult = &service.ActionResult{GameOver: true, WinningPlayerNum: 0, WinReason: "disconnect"}

			relay.resolveStaleDisconnect("g1", "p1", true)

			require.Len(t, bc.processActionCalls, 1)
			assert.Equal(t, gamelogic.ActionTypeForfeitBoth, bc.processActionCalls[0].actionType)
			assert.Equal(t, int32(1), account.calls.Load())
			assert.Equal(t, int64(0), account.body().WinnerNum)
		})

		t.Run("両者とも猶予が切れているが両者強制決着の要求が失敗するとき、EXPを付与しない", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, _ := newDisconnectResolutionRelay(entries, store)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupEntries: entries, markAwardedReturn: true}
			account := &awardCounter{}
			srv := httptest.NewServer(account.handler())
			t.Cleanup(srv.Close)
			relay.accountClient = accountclient.New(srv.URL, &http.Client{})
			bc.processActionErr = errFake

			relay.resolveStaleDisconnect("g1", "p1", true)

			require.Len(t, bc.processActionCalls, 1)
			assert.Equal(t, int32(0), account.calls.Load(), "must not award exp for a game that was left unresolved")
		})

		t.Run("NPC戦 (対戦相手が居ない)のとき、何もしない", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay([]port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "p1"}}, nil)

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})

		t.Run("ゲーム参加者情報の取得に失敗するとき、forfeitを実行しない", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay(nil, nil)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupErr: errors.New("db down")}

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})
	})
}

func TestHandleReconnect(t *testing.T) {
	t.Run("[切断・再接続]復帰処理", func(t *testing.T) {
		t.Run("対戦相手の切断猶予が切れているとき、復帰処理により対戦相手のforfeitが実行される", func(t *testing.T) {
			entries := []port.GamePlayerEntry{
				{PlayerNum: 1, PlayerID: "p1"},
				{PlayerNum: 2, PlayerID: "p2"},
			}
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, hub := newDisconnectResolutionRelay(entries, store)
			relay.JoinGame("p1", "g1", 1)
			relay.JoinGame("p2", "g1", 2)
			hub.Register(NewConnection(nil, "p1"))
			bc.processActionResult = &service.ActionResult{}

			relay.HandleReconnect("p1", "g1", false)

			require.Eventually(t, func() bool {
				return len(bc.snapshotProcessActionCalls()) == 1
			}, time.Second, 10*time.Millisecond, "stale disconnect evaluation must run")
			assert.Equal(t, 2, bc.snapshotProcessActionCalls()[0].playerNum)
		})
	})
}
