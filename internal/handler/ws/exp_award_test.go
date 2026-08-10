package ws

import (
	"context"
	"errors"
	"testing"

	game_logic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	game_design "github.com/kenyamaneko/overload-party-common/packages/game-design-constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

func TestAwardGameExp(t *testing.T) {
	t.Run("対戦終了後の経験値付与", func(t *testing.T) {
		t.Run("経験値付与の連携先が構成されていない環境のとき、付与済みかどうかの記録への問い合わせも行われないまま、何も送信されない", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{}
			relay := newTestGameRelay(t, relayDeps{accountUnconfigured: true, gamePlayerRepo: gamePlayers})

			relay.awardGameExp("game-1", 1, game_logic.WinReasonBudgetZero)

			assert.Empty(t, gamePlayers.markExpAwardedCallsSnapshot())
		})

		t.Run("付与済みかどうかの記録に失敗したとき(異常系)、経験値付与リクエストは送信されない", func(t *testing.T) {
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) {
					return false, errors.New("mark exp awarded failed")
				},
			}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, gamePlayerRepo: gamePlayers})

			relay.awardGameExp("game-1", 1, game_logic.WinReasonBudgetZero)

			assert.Empty(t, account.awardGameExpCallsSnapshot())
		})

		t.Run("同じ対戦に対して経験値付与の処理が複数回実行されたとき、アカウントサービスへの経験値付与リクエストは最初の1回のみ送信され、2回目以降は送信されない", func(t *testing.T) {
			account := &stubAccountClient{}
			awarded := false
			gamePlayers := &stubGamePlayerRepo{
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) {
					if awarded {
						return false, nil
					}
					awarded = true
					return true, nil
				},
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
			}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, gamePlayerRepo: gamePlayers})

			relay.awardGameExp("game-1", 1, game_logic.WinReasonBudgetZero)
			relay.awardGameExp("game-1", 1, game_logic.WinReasonBudgetZero)

			assert.Len(t, account.awardGameExpCallsSnapshot(), 1)
		})

		t.Run("対戦の参加者情報の取得に失敗したとき(異常系)、経験値付与リクエストは送信されない", func(t *testing.T) {
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return nil, errors.New("lookup game players failed")
				},
			}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, gamePlayerRepo: gamePlayers})

			relay.awardGameExp("game-1", 1, game_logic.WinReasonBudgetZero)

			assert.Empty(t, account.awardGameExpCallsSnapshot())
		})

		t.Run("対戦の参加者が1人のとき、経験値付与リクエストは勝敗・終了理由の情報とともに、対戦種別をNPC戦として送信される", func(t *testing.T) {
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
				},
			}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, gamePlayerRepo: gamePlayers})

			relay.awardGameExp("game-1", 1, game_logic.WinReasonBudgetZero)

			calls := account.awardGameExpCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, int64(1), calls[0].winnerNum)
			assert.Equal(t, game_logic.WinReasonBudgetZero, calls[0].reason)
			assert.Equal(t, game_design.MatchTypeNpc, calls[0].matchType)
		})

		t.Run("対戦の参加者が2人のとき、経験値付与リクエストは勝敗・終了理由の情報とともに、対戦種別をPvP戦として送信される", func(t *testing.T) {
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
			}
			relay := newTestGameRelay(t, relayDeps{accountClient: account, gamePlayerRepo: gamePlayers})

			relay.awardGameExp("game-1", 2, game_logic.WinReasonTurnTimeout)

			calls := account.awardGameExpCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, int64(2), calls[0].winnerNum)
			assert.Equal(t, game_logic.WinReasonTurnTimeout, calls[0].reason)
			assert.Equal(t, game_design.MatchTypePvp, calls[0].matchType)
			assert.ElementsMatch(t, []string{"player-1", "player-2"}, []string{calls[0].p1ID, calls[0].p2ID})
		})

		t.Run("アカウントサービスへの経験値付与リクエストの送信自体が失敗したとき(異常系)、プレイヤーには何も通知されず、再試行もされない", func(t *testing.T) {
			account := &stubAccountClient{awardGameExpFunc: func(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error {
				return errors.New("account service unavailable")
			}}
			gamePlayers := &stubGamePlayerRepo{
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, accountClient: account, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")

			relay.awardGameExp("game-1", 1, game_logic.WinReasonBudgetZero)

			assert.Len(t, account.awardGameExpCallsSnapshot(), 1)
			client.expectNoMessage(t)
		})
	})
}
