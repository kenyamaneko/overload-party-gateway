package ws

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
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

	createPvPGameMu     sync.Mutex
	createPvPGameCalls  []createPvPGameCall
	createPvPGameResult *service.GameCreatedResult
	createPvPGameErr    error
	// createPvPGameQueue, if non-nil, is popped from on each CreatePvPGame call
	// (overrides createPvPGameResult/createPvPGameErr). Enables tests where a
	// first call fails and a later retry succeeds.
	createPvPGameQueue []createPvPGameResponse

	// callsMu は processActionCalls を守る。resolveStaleDisconnect が別 goroutine
	// から ProcessAction を呼ぶ経路 (HandleReconnect 経由) があるため、書込み・読出し
	// 双方をこの mutex 経由に統一する。
	callsMu            sync.Mutex
	processActionCalls []processActionCall
}

// createPvPGameCall records a single CreatePvPGame invocation's arguments.
type createPvPGameCall struct {
	deck1Cards       []service.BattleDeckCard
	deck2Cards       []service.BattleDeckCard
	deck1Initiatives service.DeckInitiatives
	deck2Initiatives service.DeckInitiatives
	player1Summary   service.PlayerSummaryRequest
	player2Summary   service.PlayerSummaryRequest
}

type createPvPGameResponse struct {
	result *service.GameCreatedResult
	err    error
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

func (m *mockBattleClient) ListNpcModels(_ context.Context) ([]service.NpcModelEntry, error) {
	return nil, nil
}

func (m *mockBattleClient) StartNPCBattle(_ context.Context, _ []service.BattleDeckCard, _ service.DeckInitiatives, _ string, _, _ service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (m *mockBattleClient) CreatePvPGame(_ context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	m.createPvPGameMu.Lock()
	defer m.createPvPGameMu.Unlock()
	m.createPvPGameCalls = append(m.createPvPGameCalls, createPvPGameCall{
		deck1Cards:       deck1Cards,
		deck2Cards:       deck2Cards,
		deck1Initiatives: deck1Initiatives,
		deck2Initiatives: deck2Initiatives,
		player1Summary:   player1Summary,
		player2Summary:   player2Summary,
	})
	if len(m.createPvPGameQueue) > 0 {
		r := m.createPvPGameQueue[0]
		m.createPvPGameQueue = m.createPvPGameQueue[1:]
		return r.result, r.err
	}
	return m.createPvPGameResult, m.createPvPGameErr
}

// snapshotCreatePvPGameCalls は createPvPGameCalls を排他制御した上で複製して返す。
func (m *mockBattleClient) snapshotCreatePvPGameCalls() []createPvPGameCall {
	m.createPvPGameMu.Lock()
	defer m.createPvPGameMu.Unlock()
	return append([]createPvPGameCall(nil), m.createPvPGameCalls...)
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

// --- helpers ---

// newTestRelay creates a GameRelay backed by a noop hub and mock battle client.
// 依存 (account / repo / lookup) は nil のまま。EXP 付与を触るテストはフィールドに直接代入するか、
// 専用ヘルパー (exp_award_test.go の setupAwardRelay 等) を使う。
func newTestRelay() (*GameRelay, *mockBattleClient) {
	bc := newMockBattleClient()
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	}, DefaultDisconnectTimeout, nil)
	relay := NewGameRelay(hub, bc, nil, nil, nil, nil)
	return relay, bc
}

func TestRunNpcTurns(t *testing.T) {
	t.Run("NPCターンの自動進行", func(t *testing.T) {
		t.Run("初期結果がnilのとき、NPCターンを進めずnilを返す", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()

			result := relay.runNpcTurns(ctx, "g1", "p1", nil)

			assert.Nil(t, result)
			assert.Equal(t, 0, bc.advanceNpcCalls, "should not call AdvanceNpcTurn when initial result is nil")
		})

		t.Run("初期結果がNpcPending=falseのとき、ループせず同じ結果を返す", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			initial := &service.ActionResult{NpcPending: false}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			assert.Same(t, initial, result)
			assert.Equal(t, 0, bc.advanceNpcCalls, "should not loop when initial NpcPending=false")
		})

		t.Run("初期結果がGameOver=trueのとき、ループせず同じ結果を返す", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			initial := &service.ActionResult{NpcPending: true, GameOver: true}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			assert.Same(t, initial, result)
			assert.Equal(t, 0, bc.advanceNpcCalls, "should not loop when initial GameOver=true")
		})

		t.Run("NpcPendingが続くとき、not-pendingになるまでループする", func(t *testing.T) {
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

		t.Run("GameOverになったとき、即座にループを止める", func(t *testing.T) {
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

		t.Run("NPCターンの進行がエラーのとき、直前の結果を返して止める", func(t *testing.T) {
			relay, bc := newTestRelay()
			ctx := context.Background()
			bc.advanceNpcErr = errFake
			initial := &service.ActionResult{NpcPending: true}

			result := relay.runNpcTurns(ctx, "g1", "p1", initial)

			assert.Same(t, initial, result, "returns last good result on error")
			assert.Equal(t, 1, bc.advanceNpcCalls)
		})

		t.Run("ループ上限に達したとき、maxNpcTurnIterations回で止める", func(t *testing.T) {
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

// sendUseStamp は use_stamp を送信する。
func sendUseStamp(t *testing.T, conn *websocket.Conn, gameID string, stampNo int64) {
	t.Helper()
	require.NoError(t, conn.WriteJSON(WSMessage{
		Type: genws.WSClientMsgUseStamp,
		Data: mustMarshal(UseStampMessage{GameID: gameID, StampNo: stampNo}),
	}))
}

// readStampUsed は stamp_used を受信してデコードする。
func readStampUsed(t *testing.T, conn *websocket.Conn) StampUsedMessage {
	t.Helper()
	msg := readUntilType(t, conn, genws.WSServerMsgStampUsed)
	var stamp StampUsedMessage
	require.NoError(t, json.Unmarshal(msg.Data, &stamp))
	return stamp
}

func TestJoinGame(t *testing.T) {
	t.Run("ゲームへの参加とブロードキャストの到達範囲", func(t *testing.T) {
		t.Run("同じ対戦に入室した両プレイヤーへ、use_stampが両者ともに届く", func(t *testing.T) {
			cases := []struct {
				name          string
				senderIsP1    bool
				wantPlayerNum int64
			}{
				{name: "プレイヤー1が送るとき", senderIsP1: true, wantPlayerNum: 1},
				{name: "プレイヤー2が送るとき", senderIsP1: false, wantPlayerNum: 2},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					srv := newWSTestServer(t, nil)
					const gameID = "TST-GAME-JOIN-BOTH"
					srv.setupPvPGame(gameID, "uid-jb-p1", "p-jb-1", "uid-jb-p2", "p-jb-2")
					p1Conn := srv.enterGame(t, "uid-jb-p1", gameID)
					p2Conn := srv.enterGame(t, "uid-jb-p2", gameID)

					sender := p1Conn
					if !tc.senderIsP1 {
						sender = p2Conn
					}
					sendUseStamp(t, sender, gameID, 3)

					p1Stamp := readStampUsed(t, p1Conn)
					assert.EqualValues(t, tc.wantPlayerNum, p1Stamp.PlayerNum)
					assert.EqualValues(t, 3, p1Stamp.StampNo)

					p2Stamp := readStampUsed(t, p2Conn)
					assert.EqualValues(t, tc.wantPlayerNum, p2Stamp.PlayerNum)
					assert.EqualValues(t, 3, p2Stamp.StampNo)
				})
			}
		})

		t.Run("同じプレイヤーが同じ対戦へ重ねて参加しても、use_stampは1回しか届かない", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-JOIN-DUP"
			srv.setupPvPGame(gameID, "uid-jd-p1", "p-jd-1", "uid-jd-p2", "p-jd-2")
			p1Conn := srv.enterGame(t, "uid-jd-p1", gameID)
			_ = srv.enterGame(t, "uid-jd-p2", gameID)

			srv.manager.Relay.JoinGame("p-jd-1", gameID, 1)

			sendUseStamp(t, p1Conn, gameID, 5)
			readStampUsed(t, p1Conn)

			assertNoMessageWithin(t, p1Conn, 200*time.Millisecond)
		})
	})
}

func TestLeaveGame(t *testing.T) {
	t.Run("ゲームからの退出", func(t *testing.T) {
		t.Run("退出すると、退出したプレイヤーには届かなくなるが、残ったプレイヤーには届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-LEAVE-SOLO"
			srv.setupPvPGame(gameID, "uid-ls-p1", "p-ls-1", "uid-ls-p2", "p-ls-2")
			p1Conn := srv.enterGame(t, "uid-ls-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-ls-p2", gameID)
			// p2 の入室が gameID 全員に game_state/turn_controls を再配信するため、先に読み切る。
			readUntilType(t, p1Conn, genws.WSServerMsgGameState)
			readUntilType(t, p1Conn, genws.WSServerMsgTurnControls)

			srv.manager.Relay.LeaveGame("p-ls-1")

			_, ok := srv.manager.Relay.GameIDForPlayer("p-ls-1")
			assert.False(t, ok)

			sendUseStamp(t, p2Conn, gameID, 9)
			p2Stamp := readStampUsed(t, p2Conn)
			assert.EqualValues(t, 9, p2Stamp.StampNo)

			assertNoMessageWithin(t, p1Conn, 200*time.Millisecond)
		})

		t.Run("全プレイヤーが退出すると、メモリ上の対戦の参加者一覧ごと削除される", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			relay.LeaveGame("p1")
			relay.LeaveGame("p2")

			relay.mu.RLock()
			_, exists := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.False(t, exists)
		})

		t.Run("どのゲームにも居ないプレイヤーが退出しても、他のプレイヤーの参加状態は変わらない", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.JoinGame("p1", "game_1", 1)

			relay.LeaveGame("unknown_player")

			gid, ok := relay.GameIDForPlayer("p1")
			assert.True(t, ok)
			assert.Equal(t, "game_1", gid)
		})
	})
}

func TestResolvePlayerNum(t *testing.T) {
	entries := []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1"},
		{PlayerNum: 2, PlayerID: "p2"},
	}

	t.Run("プレイヤー番号の解決", func(t *testing.T) {
		t.Run("入室済みのプレイヤーのとき、入室時のスロット番号を返す", func(t *testing.T) {
			relay, _, _ := newDisconnectResolutionRelay(nil, nil)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupErr: errors.New("db down")}
			relay.JoinGame("p2", "g1", 2)

			pNum, err := relay.resolvePlayerNum(context.Background(), "g1", "p2")

			require.NoError(t, err)
			assert.Equal(t, 2, pNum)
		})

		t.Run("入室していないプレイヤーのとき、ゲームの参加者情報からスロット番号を返す", func(t *testing.T) {
			relay, _, _ := newDisconnectResolutionRelay(entries, nil)

			pNum, err := relay.resolvePlayerNum(context.Background(), "g1", "p2")

			require.NoError(t, err)
			assert.Equal(t, 2, pNum)
		})

		t.Run("別のゲームに入室しているプレイヤーのとき、指定したゲームでのスロット番号を返す", func(t *testing.T) {
			relay, _, _ := newDisconnectResolutionRelay(entries, nil)
			relay.JoinGame("p2", "g0", 1)

			pNum, err := relay.resolvePlayerNum(context.Background(), "g1", "p2")

			require.NoError(t, err)
			assert.Equal(t, 2, pNum)
		})

		t.Run("ゲームの参加者として登録されていないとき、スロットが無いことを示すエラーを返す", func(t *testing.T) {
			relay, _, _ := newDisconnectResolutionRelay(entries, nil)

			pNum, err := relay.resolvePlayerNum(context.Background(), "g1", "p9")

			assert.EqualError(t, err, "player has no slot in the game")
			assert.Equal(t, 0, pNum)
		})

		t.Run("ゲームの参加者情報の取得に失敗するとき、その原因を含むエラーを返す", func(t *testing.T) {
			relay, _, _ := newDisconnectResolutionRelay(nil, nil)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupErr: errors.New("db down")}

			pNum, err := relay.resolvePlayerNum(context.Background(), "g1", "p2")

			assert.ErrorContains(t, err, "db down")
			assert.Equal(t, 0, pNum)
		})

		t.Run("ゲームの参加者情報の取得元が無いとき、その旨のエラーを返す", func(t *testing.T) {
			relay, _ := newTestRelay()

			pNum, err := relay.resolvePlayerNum(context.Background(), "g1", "p2")

			assert.EqualError(t, err, "game player repository is not configured")
			assert.Equal(t, 0, pNum)
		})
	})
}

func TestGameIDForPlayer(t *testing.T) {
	t.Run("プレイヤーのゲームID解決", func(t *testing.T) {
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
		t.Run("呼ぶと、全プレイヤーが外れメモリ上の対戦の参加者一覧ごと削除される", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.JoinGame("p1", "game_1", 1)
			relay.JoinGame("p2", "game_1", 2)

			relay.leaveAllPlayers("game_1")

			_, ok1 := relay.GameIDForPlayer("p1")
			_, ok2 := relay.GameIDForPlayer("p2")
			assert.False(t, ok1)
			assert.False(t, ok2)

			relay.mu.RLock()
			_, exists := relay.gameMembers["game_1"]
			relay.mu.RUnlock()
			assert.False(t, exists)
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
				name:     "空スライスに追加すると、要素1つになる",
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
				name:     "nilから除去すると、nilのまま",
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
	t.Run("JSONマーシャル", func(t *testing.T) {
		t.Run("mapを渡すと、JSON文字列になる", func(t *testing.T) {
			result := mustMarshal(map[string]string{"key": "value"})
			assert.JSONEq(t, `{"key":"value"}`, string(result))
		})

		t.Run("structを渡すと、フィールドがJSONになる", func(t *testing.T) {
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

		t.Run("nilを渡すと、nullになる", func(t *testing.T) {
			result := mustMarshal(nil)
			assert.Equal(t, "null", string(result))
		})
	})
}

func TestNotifyOpponentDisconnected(t *testing.T) {
	t.Run("相手への切断通知", func(t *testing.T) {
		t.Run("membersが無いゲームのとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.NotifyOpponentDisconnected("p1", "nonexistent_game")
		})
	})
}

func TestNotifyOpponentReconnected(t *testing.T) {
	t.Run("相手への再接続通知", func(t *testing.T) {
		t.Run("membersが無いゲームのとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()

			relay.NotifyOpponentReconnected("p1", "nonexistent_game")
		})
	})
}
