package pubsub

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeWSPusher は subscriber が WSPusher 経由で行った push を記録するテスト用 fake。
// 実 ConnectionHub を使うと WebSocket 接続が必要になるため、インメモリに
// 呼び出しを蓄積して assertion できるようにする。
type fakeWSPusher struct {
	sent []sentMessage
}

type sentMessage struct {
	PlayerID string
	Msg      *WSMessage
}

func (p *fakeWSPusher) SendToPlayer(playerID string, msg *WSMessage) {
	p.sent = append(p.sent, sentMessage{PlayerID: playerID, Msg: msg})
}

// mustMarshal はテストの事前データ生成で失敗しないことを保証する。
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// コンパイル時 assertion: fakeWSPusher が WSPusher を満たすこと。
var _ WSPusher = (*fakeWSPusher)(nil)
