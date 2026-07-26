package ws

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// noopMatchmakingClient は port.MatchmakingClient のテスト用実装。Hub.Unregister が
// 常に呼ぶ OnMatchmakingLeave の配線先を満たすためだけに使う。
type noopMatchmakingClient struct{}

func (noopMatchmakingClient) Enqueue(_ context.Context, _ int64, _ string, _ int64) error { return nil }
func (noopMatchmakingClient) Cancel(_ context.Context) error                             { return nil }

var _ port.MatchmakingClient = noopMatchmakingClient{}

func TestManagerReconnect(t *testing.T) {
	t.Run("切断猶予切れ状態での復帰時の決着評価", func(t *testing.T) {
		t.Run("対戦相手の猶予切れのまま再接続すると、対戦相手の forfeit が実行される", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.processActionResult = &service.ActionResult{}
			repo := &mockGamePlayerRepo{lookupEntries: []port.GamePlayerEntry{
				{PlayerNum: 1, PlayerID: "p1"},
				{PlayerNum: 2, PlayerID: "p2"},
			}}
			// 最初の接続時点では猶予期限の写しがまだ無い (found=false)。Unregister で
			// 実際に切断させたあと、対戦相手 (p2) が猶予切れであるという状況を想定して
			// 応答を書き換える。
			store := &fakeTimerStore{getDisconnectFound: false}

			m := NewManager(bc, nil, nil, noopMatchmakingClient{}, repo, 0, nil, store)
			m.Relay.JoinGame("p1", "g1", 1)
			m.Relay.JoinGame("p2", "g1", 2)

			connP1 := NewConnection(nil, "p1")
			m.Hub.Register(connP1)
			m.Hub.Unregister(connP1)

			store.getDisconnectFound = true
			store.getDisconnectReturn = portDisconnectDeadline("g1", time.Now().Add(-time.Minute))

			m.Hub.Register(NewConnection(nil, "p1"))

			require.Eventually(t, func() bool {
				return len(bc.snapshotProcessActionCalls()) == 1
			}, time.Second, 10*time.Millisecond, "opponent forfeit must fire")
			calls := bc.snapshotProcessActionCalls()
			assert.Equal(t, 2, calls[0].playerNum, "forfeit must be attributed to the still-disconnected opponent")
			assert.Equal(t, gamelogic.ActionTypeForfeit, calls[0].actionType)
		})
	})
}
