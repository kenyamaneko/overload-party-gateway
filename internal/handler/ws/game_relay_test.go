package ws

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

var errFake = errors.New("fake battle error")

// --- Mock BattleClient ---

type mockBattleClient struct {
	processActionResult *service.ActionResult
	processActionErr    error
	turnControls        json.RawMessage
	turnControlsErr     error
	advanceNpcResult    *service.ActionResult
	advanceNpcErr       error
	// advanceNpcQueue, if non-nil, is popped from on each AdvanceNpcTurn call
	// (overrides advanceNpcResult). Enables tests that need successive results.
	advanceNpcQueue []*service.ActionResult
	advanceNpcCalls int

	// callsMu は processActionCalls を守る。resolveStaleDisconnect が別 goroutine
	// から ProcessAction を呼ぶ経路 (HandleReconnect 経由) があるため、書込み・読出し
	// 双方をこの mutex 経由に統一する。
	callsMu            sync.Mutex
	processActionCalls []processActionCall
}

// processActionCall は mockBattleClient.ProcessAction への 1 回の呼出を記録する。
type processActionCall struct {
	gameID     string
	playerNum  int
	actionType string
	data       json.RawMessage
}

func newMockBattleClient() *mockBattleClient {
	return &mockBattleClient{}
}

func (m *mockBattleClient) GetNPCModels(_ context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[]`), nil
}

func (m *mockBattleClient) ListNpcModels(_ context.Context) ([]service.NpcModelEntry, error) {
	return nil, nil
}

func (m *mockBattleClient) StartNPCBattle(_ context.Context, _ []service.BattleDeckCard, _ service.DeckInitiatives, _ string, _, _ service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (m *mockBattleClient) CreatePvPGame(_ context.Context, _, _ []service.BattleDeckCard, _, _ service.DeckInitiatives, _, _ service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (m *mockBattleClient) ProcessAction(_ context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
	m.callsMu.Lock()
	m.processActionCalls = append(m.processActionCalls, processActionCall{
		gameID: gameID, playerNum: playerNum, actionType: actionType, data: data,
	})
	m.callsMu.Unlock()
	return m.processActionResult, m.processActionErr
}

// snapshotProcessActionCalls は processActionCalls を排他制御した上で複製して返す。
// resolveStaleDisconnect が別 goroutine から書き込みうる経路を検証するテストは、
// 直接フィールドを読まずこの関数を使う。
func (m *mockBattleClient) snapshotProcessActionCalls() []processActionCall {
	m.callsMu.Lock()
	defer m.callsMu.Unlock()
	return append([]processActionCall(nil), m.processActionCalls...)
}

func (m *mockBattleClient) GetGameStateForPlayer(_ context.Context, _ string, _ int) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (m *mockBattleClient) GetTurnControlsForPlayer(_ context.Context, _ string, _ int) (json.RawMessage, error) {
	return m.turnControls, m.turnControlsErr
}

func (m *mockBattleClient) AdvanceNpcTurn(_ context.Context, _ string) (*service.ActionResult, error) {
	m.advanceNpcCalls++
	// queue モードと固定エラーモードは排他。混在するとテストの意図が曖昧になるため、
	// セットアップミスを早期検出する。
	if m.advanceNpcQueue != nil && m.advanceNpcErr != nil {
		panic("mockBattleClient: advanceNpcQueue と advanceNpcErr の同時セットは禁止（モード排他）")
	}
	if m.advanceNpcQueue != nil {
		if len(m.advanceNpcQueue) == 0 {
			panic("mockBattleClient: advanceNpcQueue exhausted — test setup has fewer results than AdvanceNpcTurn calls")
		}
		r := m.advanceNpcQueue[0]
		m.advanceNpcQueue = m.advanceNpcQueue[1:]
		return r, nil
	}
	return m.advanceNpcResult, m.advanceNpcErr
}

func (m *mockBattleClient) GetGameLog(_ context.Context, _ string) (json.RawMessage, error) {
	return nil, nil
}

func (m *mockBattleClient) GetGameLogText(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

// --- helpers ---

// newTestRelay creates a GameRelay backed by a noop hub and mock battle client.
// 依存 (account / repo / lookup) は nil のまま。EXP 付与を触るテストはフィールドに直接代入するか、
// 専用ヘルパー (exp_award_test.go の setupAwardRelay 等) を使う。
func newTestRelay() (*GameRelay, *mockBattleClient) {
	bc := newMockBattleClient()
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	}, nil)
	relay := NewGameRelay(hub, bc, nil, nil, nil)
	return relay, bc
}

func TestBattleStateMeta_Parsing(t *testing.T) {
	t.Run("battle 状態 JSON の最小射影パース", func(t *testing.T) {
		tests := []struct {
			name            string
			raw             string
			wantCurrentTurn int64
			wantIsMyTurn    bool
			wantTimeBank    int64
		}{
			{
				name: "全フィールドを含む JSON のとき、currentTurn/isMyTurn/timeBank を取り出す",
				raw: `{
					"currentTurn": 5,
					"isMyTurn": true,
					"myView": {
						"timeBank": 120,
						"hand": [1,2,3],
						"field": {"cards": []}
					},
					"opponentView": {"handCount": 4}
				}`,
				wantCurrentTurn: 5,
				wantIsMyTurn:    true,
				wantTimeBank:    120,
			},
			{
				name: "isMyTurn=false の JSON のとき、そのまま反映する",
				raw: `{
					"currentTurn": 3,
					"isMyTurn": false,
					"myView": {"timeBank": 0}
				}`,
				wantCurrentTurn: 3,
				wantIsMyTurn:    false,
				wantTimeBank:    0,
			},
			{
				name:            "空 JSON のとき、ゼロ値になる",
				raw:             `{}`,
				wantCurrentTurn: 0,
				wantIsMyTurn:    false,
				wantTimeBank:    0,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var meta battleStateMeta
				err := json.Unmarshal(json.RawMessage(tt.raw), &meta)
				require.NoError(t, err)
				assert.Equal(t, tt.wantCurrentTurn, meta.CurrentTurn)
				assert.Equal(t, tt.wantIsMyTurn, meta.IsMyTurn)
				assert.Equal(t, tt.wantTimeBank, meta.MyView.TimeBank)
			})
		}
	})
}

func TestRunNpcTurns(t *testing.T) {
	t.Run("NPC ターンの自動進行", func(t *testing.T) {
		t.Run("初期結果が nil のとき、AdvanceNpcTurn を呼ばず nil を返す", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()

			result := relay.runNpcTurns(ctx, "g1", "p1", nil)

			assert.Nil(t, result)
			assert.Equal(t, 0, bc.advanceNpcCalls, "should not call AdvanceNpcTurn when initial result is nil")
		})

		t.Run("初期結果が NpcPending=false のとき、ループせず同じ結果を返す", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			initial := &service.ActionResult{NpcPending: false}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			assert.Same(t, initial, result)
			assert.Equal(t, 0, bc.advanceNpcCalls, "should not loop when initial NpcPending=false")
		})

		t.Run("初期結果が GameOver=true のとき、ループせず同じ結果を返す", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			initial := &service.ActionResult{NpcPending: true, GameOver: true}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			assert.Same(t, initial, result)
			assert.Equal(t, 0, bc.advanceNpcCalls, "should not loop when initial GameOver=true")
		})

		t.Run("NpcPending が続くとき、not-pending になるまでループする", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			bc.advanceNpcQueue = []*service.ActionResult{
				{NpcPending: true},  // second NPC action, still pending
				{NpcPending: false}, // third call resolves — control back to human
			}
			initial := &service.ActionResult{NpcPending: true}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			require.NotNil(t, result)
			assert.False(t, result.NpcPending)
			assert.False(t, result.GameOver)
			assert.Equal(t, 2, bc.advanceNpcCalls)
		})

		t.Run("GameOver になったとき、即座にループを止める", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			bc.advanceNpcQueue = []*service.ActionResult{
				{NpcPending: true, GameOver: true, WinningPlayerNum: 2, WinReason: "lp_zero"},
			}
			initial := &service.ActionResult{NpcPending: true}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			require.NotNil(t, result)
			assert.True(t, result.GameOver)
			assert.Equal(t, int64(2), result.WinningPlayerNum)
			assert.Equal(t, 1, bc.advanceNpcCalls, "should stop immediately on GameOver")
		})

		t.Run("AdvanceNpcTurn がエラーのとき、直前の結果を返して止める", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			bc.advanceNpcErr = errFake
			initial := &service.ActionResult{NpcPending: true}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			assert.Same(t, initial, result, "returns last good result on error")
			assert.Equal(t, 1, bc.advanceNpcCalls)
		})

		t.Run("ループ上限に達したとき、maxNpcTurnIterations 回で止める", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()

			// キューはループ上限を1つ超えるサイズで用意する。上限到達時に i==maxNpcTurnIterations で
			// 抜けるため、最後の1件はポップされない。
			queue := make([]*service.ActionResult, maxNpcTurnIterations+1)
			for i := range queue {
				queue[i] = &service.ActionResult{NpcPending: true}
			}
			bc.advanceNpcQueue = queue
			initial := &service.ActionResult{NpcPending: true}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			require.NotNil(t, result)
			assert.True(t, result.NpcPending, "cap interrupts the loop so NpcPending remains true")
			assert.Equal(t, maxNpcTurnIterations, bc.advanceNpcCalls, "should stop after exactly maxNpcTurnIterations calls")
		})
	})
}

func TestJoinGame(t *testing.T) {
	t.Run("ゲームへの参加", func(t *testing.T) {
		t.Run("参加すると、playerGames と gameMembers に登録される", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)

			gid, ok := relay.GameIDForPlayer("p1")
			assert.True(t, ok)
			assert.Equal(t, "game_1", gid)

			relay.mu.RLock()
			members := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.Contains(t, members, "p1")
		})

		t.Run("2 人が同じゲームに参加すると、両者が members に入る", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			relay.mu.RLock()
			members := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.Len(t, members, 2)
			assert.Contains(t, members, "p1")
			assert.Contains(t, members, "p2")
		})

		t.Run("同じプレイヤーが重複参加しても、members に二重登録されない", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p1", "game_1", 1)

			relay.mu.RLock()
			members := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.Len(t, members, 1, "duplicate join should not add player twice")
		})

		t.Run("参加時に playerNum がキャッシュされ、resolvePlayerNum で引ける", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			assert.Equal(t, 1, relay.resolvePlayerNum("p1"))
			assert.Equal(t, 2, relay.resolvePlayerNum("p2"))
		})

		t.Run("別ゲームに参加し直すと、前のゲームの members から外れる", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p1", "game_2", 1)

			gid, ok := relay.GameIDForPlayer("p1")
			assert.True(t, ok)
			assert.Equal(t, "game_2", gid)

			relay.mu.RLock()
			members2 := relay.gameMembers["game_2"]
			members1 := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.Contains(t, members2, "p1")
			assert.NotContains(t, members1, "p1")
		})
	})
}

func TestLeaveGame(t *testing.T) {
	t.Run("ゲームからの退出", func(t *testing.T) {
		t.Run("退出すると、そのプレイヤーだけがゲームから外れる", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			relay.LeaveGame("p1")

			_, ok := relay.GameIDForPlayer("p1")
			assert.False(t, ok)

			gid, ok := relay.GameIDForPlayer("p2")
			assert.True(t, ok)
			assert.Equal(t, "game_1", gid)

			relay.mu.RLock()
			members := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.Len(t, members, 1)
			assert.Contains(t, members, "p2")
		})

		t.Run("最後のプレイヤーが退出すると、gameMembers が破棄される", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			relay.LeaveGame("p1")
			relay.LeaveGame("p2")

			relay.mu.RLock()
			_, gameMembersExist := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.False(t, gameMembersExist, "gameMembers should be cleaned up when last player leaves")
		})

		t.Run("どのゲームにも居ないプレイヤーが退出しても、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.LeaveGame("unknown_player")

			_, ok := relay.GameIDForPlayer("unknown_player")
			assert.False(t, ok)
		})
	})
}

func TestResolvePlayerNum(t *testing.T) {
	t.Run("プレイヤー番号の解決", func(t *testing.T) {
		t.Run("ゲームに居ないプレイヤーのとき、0 を返す", func(t *testing.T) {
			relay, _ := newTestRelay()
			assert.Equal(t, 0, relay.resolvePlayerNum("unknown"))
		})
	})
}

func TestGameIDForPlayer(t *testing.T) {
	t.Run("プレイヤーのゲーム ID 解決", func(t *testing.T) {
		t.Run("参加前は無く、参加後に取得でき、退出後に再び無くなる", func(t *testing.T) {
			relay, _ := newTestRelay()

			_, ok := relay.GameIDForPlayer("p1")
			assert.False(t, ok)

			relay.JoinGame("p1", "game_1", 1)
			gid, ok := relay.GameIDForPlayer("p1")
			assert.True(t, ok)
			assert.Equal(t, "game_1", gid)

			relay.LeaveGame("p1")
			_, ok = relay.GameIDForPlayer("p1")
			assert.False(t, ok)
		})
	})
}

func TestLeaveAllPlayers(t *testing.T) {
	t.Run("全プレイヤーの一括退出", func(t *testing.T) {
		t.Run("呼ぶと、全プレイヤーが外れ gameMembers も破棄される", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			relay.leaveAllPlayers("game_1")

			_, ok1 := relay.GameIDForPlayer("p1")
			_, ok2 := relay.GameIDForPlayer("p2")
			assert.False(t, ok1)
			assert.False(t, ok2)

			relay.mu.RLock()
			_, membersExist := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.False(t, membersExist, "gameMembers should be cleaned up")
		})

		t.Run("呼ぶと、退出した全プレイヤーの切断猶予期限の写しが削除される", func(t *testing.T) {
			relay, _ := newTestRelay()
			store := &fakeTimerStore{}
			relay.hub.timerStore = store

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			relay.leaveAllPlayers("game_1")

			assert.ElementsMatch(t, []string{"p1", "p2"}, store.snapshotClearDisconnectCalls())
		})
	})
}

func TestAppendUnique(t *testing.T) {
	t.Run("重複なし追加", func(t *testing.T) {
		tests := []struct {
			name     string
			initial  []string
			add      string
			expected []string
		}{
			{
				name:     "空スライスに追加すると、要素 1 つになる",
				initial:  nil,
				add:      "a",
				expected: []string{"a"},
			},
			{
				name:     "新しい要素を追加すると、末尾に追加される",
				initial:  []string{"a", "b"},
				add:      "c",
				expected: []string{"a", "b", "c"},
			},
			{
				name:     "既存要素を追加すると、追加されない",
				initial:  []string{"a", "b"},
				add:      "a",
				expected: []string{"a", "b"},
			},
			{
				name:     "末尾と同じ要素を追加すると、追加されない",
				initial:  []string{"a", "b"},
				add:      "b",
				expected: []string{"a", "b"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := appendUnique(tt.initial, tt.add)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestRemoveString(t *testing.T) {
	t.Run("文字列スライスからの除去", func(t *testing.T) {
		tests := []struct {
			name     string
			initial  []string
			remove   string
			expected []string
		}{
			{
				name:     "存在する要素を除去すると、その要素が消える",
				initial:  []string{"a", "b", "c"},
				remove:   "b",
				expected: []string{"a", "c"},
			},
			{
				name:     "先頭要素を除去すると、先頭が消える",
				initial:  []string{"a", "b", "c"},
				remove:   "a",
				expected: []string{"b", "c"},
			},
			{
				name:     "末尾要素を除去すると、末尾が消える",
				initial:  []string{"a", "b", "c"},
				remove:   "c",
				expected: []string{"a", "b"},
			},
			{
				name:     "単一要素から除去すると、空スライスになる",
				initial:  []string{"a"},
				remove:   "a",
				expected: []string{},
			},
			{
				name:     "存在しない要素を除去すると、変化しない",
				initial:  []string{"a", "b"},
				remove:   "z",
				expected: []string{"a", "b"},
			},
			{
				name:     "空スライスから除去すると、空のまま",
				initial:  []string{},
				remove:   "a",
				expected: []string{},
			},
			{
				name:     "nil から除去すると、nil のまま",
				initial:  nil,
				remove:   "a",
				expected: nil,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := removeString(tt.initial, tt.remove)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestMustMarshal(t *testing.T) {
	t.Run("JSON マーシャル", func(t *testing.T) {
		t.Run("map を渡すと、JSON 文字列になる", func(t *testing.T) {
			result := mustMarshal(map[string]string{"key": "value"})
			assert.JSONEq(t, `{"key":"value"}`, string(result))
		})

		t.Run("struct を渡すと、フィールドが JSON になる", func(t *testing.T) {
			msg := ErrorMessage{ErrorCode: "test", Message: "hello", Retryable: true}
			result := mustMarshal(msg)
			require.NotNil(t, result)

			var parsed ErrorMessage
			err := json.Unmarshal(result, &parsed)
			require.NoError(t, err)
			assert.Equal(t, "test", parsed.ErrorCode)
			assert.Equal(t, "hello", parsed.Message)
			assert.True(t, parsed.Retryable)
		})

		t.Run("nil を渡すと、null になる", func(t *testing.T) {
			result := mustMarshal(nil)
			assert.Equal(t, "null", string(result))
		})
	})
}

func TestNotifyOpponentDisconnected(t *testing.T) {
	t.Run("相手への切断通知", func(t *testing.T) {
		t.Run("members が無いゲームのとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.NotifyOpponentDisconnected("p1", "nonexistent_game")
		})
	})
}

func TestNotifyOpponentReconnected(t *testing.T) {
	t.Run("相手への再接続通知", func(t *testing.T) {
		t.Run("members が無いゲームのとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.NotifyOpponentReconnected("p1", "nonexistent_game")
		})
	})
}
