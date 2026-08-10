package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	game_logic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

func flattenMarkInvalidatedCalls(calls [][]string) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c...)
	}
	return out
}

func TestInvalidateActiveGames(t *testing.T) {
	t.Run("停止時の進行中対戦の無効化記録", func(t *testing.T) {
		t.Run("無効化記録の機能が構成されていない環境のとき、対戦参加者情報への問い合わせも行われないまま、無効化の記録は行われない", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepoUnconfigured: true, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-1", 1)

			relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, gamePlayers.countPlayersByGameCallsSnapshot())
		})

		t.Run("対戦参加者情報の取得先が構成されていない環境のとき、無効化の記録は行われない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, gamePlayerRepoUnconfigured: true})
			relay.JoinGame("player-1", "game-1", 1)

			relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, flattenMarkInvalidatedCalls(invalidated.markInvalidatedCallsSnapshot()))
		})

		t.Run("進行中の対戦が無いとき、無効化の記録は行われない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			gamePlayers := &stubGamePlayerRepo{countPlayersByGameFunc: func(ctx context.Context, gameIDs []string) (map[string]int, error) {
				return map[string]int{}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})

			relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, flattenMarkInvalidatedCalls(invalidated.markInvalidatedCallsSnapshot()))
		})

		t.Run("進行中の対戦の参加者情報が見つからないとき、その対戦は無効として記録されない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			gamePlayers := &stubGamePlayerRepo{countPlayersByGameFunc: func(ctx context.Context, gameIDs []string) (map[string]int, error) {
				return map[string]int{}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-not-found", 1)

			relay.InvalidateActiveGames(context.Background())

			assert.NotContains(t, flattenMarkInvalidatedCalls(invalidated.markInvalidatedCallsSnapshot()), "game-not-found")
		})

		t.Run("進行中の対戦が全て人間プレイヤー1人の対戦(NPC戦)のとき、無効化の記録は行われない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			gamePlayers := &stubGamePlayerRepo{countPlayersByGameFunc: func(ctx context.Context, gameIDs []string) (map[string]int, error) {
				return map[string]int{"game-npc": 1}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-npc", 1)

			relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, flattenMarkInvalidatedCalls(invalidated.markInvalidatedCallsSnapshot()))
		})

		t.Run("進行中の対戦の中に人間プレイヤーが2人揃った対戦(PvP戦)が含まれるとき、そのPvP戦だけが無効として記録される(NPC戦は記録されない)", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			gamePlayers := &stubGamePlayerRepo{countPlayersByGameFunc: func(ctx context.Context, gameIDs []string) (map[string]int, error) {
				return map[string]int{"game-pvp": 2, "game-npc": 1}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-pvp", 1)
			relay.JoinGame("player-2", "game-npc", 1)

			relay.InvalidateActiveGames(context.Background())

			marked := flattenMarkInvalidatedCalls(invalidated.markInvalidatedCallsSnapshot())
			assert.Contains(t, marked, "game-pvp")
			assert.NotContains(t, marked, "game-npc")
		})

		t.Run("進行中の対戦の人数集計に失敗したとき(異常系)、どの対戦も無効として記録されない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			gamePlayers := &stubGamePlayerRepo{countPlayersByGameFunc: func(ctx context.Context, gameIDs []string) (map[string]int, error) {
				return nil, errors.New("count players failed")
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-1", 1)

			relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, flattenMarkInvalidatedCalls(invalidated.markInvalidatedCallsSnapshot()))
		})

		t.Run("無効化の記録の書き込みに失敗したとき(異常系)、対象の対戦への書き込みは試みられた上で、処理はパニックせず終了する", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{markInvalidatedFunc: func(ctx context.Context, gameIDs []string) error {
				return errors.New("mark invalidated failed")
			}}
			gamePlayers := &stubGamePlayerRepo{countPlayersByGameFunc: func(ctx context.Context, gameIDs []string) (map[string]int, error) {
				return map[string]int{"game-pvp": 2}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-pvp", 1)

			assert.NotPanics(t, func() {
				relay.InvalidateActiveGames(context.Background())
			})

			calls := invalidated.markInvalidatedCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, []string{"game-pvp"}, calls[0])
		})
	})
}

func TestFinishInvalidatedGames(t *testing.T) {
	t.Run("無効化された対戦の決着(起動時復旧)", func(t *testing.T) {
		t.Run("無効化記録の機能が構成されていない環境のとき、復旧処理は何も行わない", func(t *testing.T) {
			battle := &stubBattleClient{}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepoUnconfigured: true, battleClient: battle})

			relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, battle.processActionCallsSnapshot())
		})

		t.Run("決着していない無効な対戦の一覧取得に失敗したとき(異常系)、決着処理は行われない", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{listUnfinishedFunc: func(ctx context.Context) ([]string, error) {
				return nil, errors.New("list unfinished failed")
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, battleClient: battle})

			relay.finishInvalidatedGames(context.Background())

			assert.Empty(t, battle.processActionCallsSnapshot())
		})

		t.Run("決着していない無効な対戦が0件のとき、決着処理は行われない", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{listUnfinishedFunc: func(ctx context.Context) ([]string, error) {
				return []string{}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, battleClient: battle})

			relay.finishInvalidatedGames(context.Background())

			assert.Empty(t, battle.processActionCallsSnapshot())
		})

		t.Run("決着していない無効な対戦の対戦サービスでの強制決着が成功したとき、その対戦は決着済みとして記録され、対戦の全参加者の切断猶予期限が消える", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: true, WinReason: game_logic.WinReasonSystemDown}, nil
			}}
			invalidated := &stubInvalidatedGameRepo{listUnfinishedFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-1"}, nil
			}}
			timerStore := &stubTimerStore{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			relay := newTestGameRelay(t, relayDeps{hub: hub, invalidatedGameRepo: invalidated, battleClient: battle, gamePlayerRepo: gamePlayers})

			relay.finishInvalidatedGames(context.Background())

			assert.Contains(t, invalidated.markFinishedCallsSnapshot(), "game-1")
			cleared := timerStore.clearDisconnectDeadlineCallsSnapshot()
			assert.Contains(t, cleared, "player-1")
			assert.Contains(t, cleared, "player-2")

			calls := battle.processActionCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, "game-1", calls[0].gameID)
			assert.Equal(t, forfeitBothPlayerNum, calls[0].playerNum)
			assert.Equal(t, game_logic.ActionTypeForfeitBoth, calls[0].actionType)
		})

		t.Run("決着していない無効な対戦の対戦サービスでの強制決着が失敗したとき(異常系)、その対戦は決着済みとして記録されず、参加者の切断猶予期限も消えない", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return nil, errors.New("process action failed")
			}}
			invalidated := &stubInvalidatedGameRepo{listUnfinishedFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-1"}, nil
			}}
			timerStore := &stubTimerStore{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			relay := newTestGameRelay(t, relayDeps{hub: hub, invalidatedGameRepo: invalidated, battleClient: battle, gamePlayerRepo: gamePlayers})

			relay.finishInvalidatedGames(context.Background())

			assert.NotContains(t, invalidated.markFinishedCallsSnapshot(), "game-1")
			assert.Empty(t, timerStore.clearDisconnectDeadlineCallsSnapshot())
		})

		t.Run("強制決着は成功したが、決着済みとして記録する処理自体が失敗したとき(異常系)、参加者の切断猶予期限は消えない", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: true}, nil
			}}
			invalidated := &stubInvalidatedGameRepo{
				listUnfinishedFunc: func(ctx context.Context) ([]string, error) { return []string{"game-1"}, nil },
				markFinishedFunc: func(ctx context.Context, gameID string) error {
					return errors.New("mark finished failed")
				},
			}
			timerStore := &stubTimerStore{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			relay := newTestGameRelay(t, relayDeps{hub: hub, invalidatedGameRepo: invalidated, battleClient: battle, gamePlayerRepo: gamePlayers})

			relay.finishInvalidatedGames(context.Background())

			assert.Empty(t, timerStore.clearDisconnectDeadlineCallsSnapshot())
		})

		t.Run("決着していない無効な対戦が複数あり、そのうち一部の決着に失敗したとき、失敗した対戦は決着済みとして記録されない一方、他の対戦の決着処理は影響を受けず行われる", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				if gameID == "game-fail" {
					return nil, errors.New("process action failed")
				}
				return &service.ActionResult{GameOver: true}, nil
			}}
			invalidated := &stubInvalidatedGameRepo{listUnfinishedFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-fail", "game-ok"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated, battleClient: battle, gamePlayerRepo: gamePlayers})

			relay.finishInvalidatedGames(context.Background())

			finished := invalidated.markFinishedCallsSnapshot()
			assert.Contains(t, finished, "game-ok")
			assert.NotContains(t, finished, "game-fail")
		})
	})
}

func TestRevertInvalidatedGameBattleCounts(t *testing.T) {
	t.Run("無効化された対戦の消費バトル回数の払い戻し(起動時復旧)", func(t *testing.T) {
		t.Run("アカウントサービスとの連携先が構成されていない環境のとき、払い戻し待ちの対戦の問い合わせも行われないまま、払い戻しは行われない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			relay := newTestGameRelay(t, relayDeps{accountUnconfigured: true, invalidatedGameRepo: invalidated})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			assert.Zero(t, invalidated.listRevertPendingCallCount())
		})

		t.Run("対戦参加者情報の取得先が構成されていない環境のとき、払い戻しは行われない", func(t *testing.T) {
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-1"}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepoUnconfigured: true})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			assert.Empty(t, account.revertBattleCountCallsSnapshot())
		})

		t.Run("払い戻し待ちの対戦の一覧取得に失敗したとき(異常系)、払い戻しは行われない", func(t *testing.T) {
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return nil, errors.New("list revert pending failed")
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			assert.Empty(t, account.revertBattleCountCallsSnapshot())
		})

		t.Run("払い戻し待ちの対戦が0件のとき、何も行われない", func(t *testing.T) {
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return []string{}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			assert.Empty(t, account.revertBattleCountCallsSnapshot())
		})

		t.Run("払い戻し待ちの対戦の参加者情報の取得に失敗したとき(異常系)、その対戦の払い戻しは行われない", func(t *testing.T) {
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, errors.New("lookup game players failed")
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			assert.Empty(t, account.revertBattleCountCallsSnapshot())
		})

		t.Run("払い戻し待ちの対戦の参加者に人間プレイヤーが2人揃わない(片方以上が存在しない)とき、その対戦の払い戻しは行われない", func(t *testing.T) {
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-npc"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			assert.Empty(t, account.revertBattleCountCallsSnapshot())
		})

		t.Run("払い戻し待ちの対戦に人間プレイヤーが2人揃っているとき、アカウントサービスへ両プレイヤーの消費バトル回数を戻すリクエストが送信され、成功すると払い戻し済みとして記録される", func(t *testing.T) {
			earliest := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
			later := earliest.Add(time.Minute)
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "player-1", CreatedAt: later},
					{PlayerNum: 2, PlayerID: "player-2", CreatedAt: earliest},
				}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			calls := account.revertBattleCountCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, "game-1", calls[0].gameID)
			assert.ElementsMatch(t, []string{"player-1", "player-2"}, []string{calls[0].p1ID, calls[0].p2ID})
			assert.True(t, calls[0].consumedAt.Equal(earliest))
			assert.Contains(t, invalidated.markRevertedCallsSnapshot(), "game-1")
		})

		t.Run("払い戻しのリクエスト自体が失敗したとき(異常系)、払い戻し済みとして記録されない", func(t *testing.T) {
			account := &stubAccountClient{revertBattleCountFunc: func(ctx context.Context, gameID, p1ID, p2ID string, consumedAt time.Time) error {
				return errors.New("revert battle count failed")
			}}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			assert.NotContains(t, invalidated.markRevertedCallsSnapshot(), "game-1")
		})

		t.Run("払い戻しのリクエストは成功したが、払い戻し済みとして記録する処理が失敗したとき(異常系)、払い戻し済みとして記録されない", func(t *testing.T) {
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{
				listRevertPendingFunc: func(ctx context.Context) ([]string, error) { return []string{"game-1"}, nil },
				markRevertedFunc: func(ctx context.Context, gameID string) error {
					return errors.New("mark reverted failed")
				},
			}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			require.Len(t, account.revertBattleCountCallsSnapshot(), 1)
		})

		t.Run("払い戻し待ちの対戦が複数あり、そのうち一部の払い戻しに失敗したとき、失敗した対戦は払い戻し済みとして記録されない一方、他の対戦の処理は影響を受けず行われる", func(t *testing.T) {
			account := &stubAccountClient{revertBattleCountFunc: func(ctx context.Context, gameID, p1ID, p2ID string, consumedAt time.Time) error {
				if gameID == "game-fail" {
					return errors.New("revert battle count failed")
				}
				return nil
			}}
			invalidated := &stubInvalidatedGameRepo{listRevertPendingFunc: func(ctx context.Context) ([]string, error) {
				return []string{"game-fail", "game-ok"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})

			relay.revertInvalidatedGameBattleCounts(context.Background())

			reverted := invalidated.markRevertedCallsSnapshot()
			assert.Contains(t, reverted, "game-ok")
			assert.NotContains(t, reverted, "game-fail")
		})
	})
}

func TestEarliestCreatedAt(t *testing.T) {
	t.Run("対戦参加記録の中で最も早い作成時刻の算出", func(t *testing.T) {
		t.Run("参加記録が複数件あり作成時刻が異なるとき、算出結果はその中で最も早い時刻になる", func(t *testing.T) {
			earliest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			later := earliest.Add(time.Hour)
			entries := []port.GamePlayerEntry{
				{PlayerNum: 1, PlayerID: "player-1", CreatedAt: later},
				{PlayerNum: 2, PlayerID: "player-2", CreatedAt: earliest},
			}

			got := earliestCreatedAt(entries)

			assert.True(t, got.Equal(earliest))
		})

		t.Run("参加記録が複数件あり作成時刻が同じとき、算出結果はその時刻になる", func(t *testing.T) {
			same := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			entries := []port.GamePlayerEntry{
				{PlayerNum: 1, PlayerID: "player-1", CreatedAt: same},
				{PlayerNum: 2, PlayerID: "player-2", CreatedAt: same},
			}

			got := earliestCreatedAt(entries)

			assert.True(t, got.Equal(same))
		})
	})
}

func TestIsGameInvalidated(t *testing.T) {
	t.Run("対戦が無効化済みかどうかの判定", func(t *testing.T) {
		t.Run("無効化記録の機能が構成されていない環境のとき、対戦は無効化されていないと判定される", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepoUnconfigured: true})

			got, err := relay.isGameInvalidated(context.Background(), "game-1")

			require.NoError(t, err)
			assert.False(t, got)
		})

		t.Run("機能が構成されており、対象の対戦が無効として記録されているとき、無効化されていると判定される", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) {
				return true, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated})

			got, err := relay.isGameInvalidated(context.Background(), "game-1")

			require.NoError(t, err)
			assert.True(t, got)
		})

		t.Run("機能が構成されており、対象の対戦が無効として記録されていないとき、無効化されていないと判定される", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) {
				return false, nil
			}}
			relay := newTestGameRelay(t, relayDeps{invalidatedGameRepo: invalidated})

			got, err := relay.isGameInvalidated(context.Background(), "game-1")

			require.NoError(t, err)
			assert.False(t, got)
		})
	})
}
