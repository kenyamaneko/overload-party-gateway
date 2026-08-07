package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingfake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMatchHandler は port.MatchEventHandler のテスト用スタブ。
// 受信した event と error を記録し、テストから振る舞いを制御できる。
type fakeMatchHandler struct {
	mu       sync.Mutex
	received []apimatchmaking.MatchMadeEvent
	err      error
}

func (h *fakeMatchHandler) HandleMatchMade(_ context.Context, event apimatchmaking.MatchMadeEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.received = append(h.received, event)
	return h.err
}

func (h *fakeMatchHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.received)
}

// wireBytes は matchmaking 送信側 fake 経由で MatchMadeEvent を実際の wire 形式へ
// marshal する。matchmaking が schema を変えたら本テストが compile / 実行で
// 破綻して乖離を検知できるようにするため、直接 json.Marshal せずこの経路を使う。
func wireBytes(t *testing.T, ev apimatchmaking.MatchMadeEvent) []byte {
	t.Helper()
	pub := apimatchmakingfake.NewPublisher(apimatchmakingfake.NewBroker())
	require.NoError(t, apimatchmakingfake.PublishMatchMade(context.Background(), pub, ev))
	published := pub.Published()
	require.Len(t, published, 1)
	return published[0].Data
}

func TestMatchSubscriber_ProcessMessage(t *testing.T) {
	validPlayers := []apimatchmaking.MatchedPlayer{
		{PlayerID: "p-1", DeckID: 10},
		{PlayerID: "p-2", DeckID: 20},
	}

	t.Run("[マッチング]マッチ成立イベントの処理", func(t *testing.T) {
		t.Run("有効なマッチ成立イベントのとき、エラーを返さず受信した内容をそのまま処理に渡す", func(t *testing.T) {
			handler := &fakeMatchHandler{}
			sub, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			data := wireBytes(t, apimatchmaking.MatchMadeEvent{MatchID: "mch_1", Players: validPlayers})

			err = sub.ProcessMessage(context.Background(), data)

			require.NoError(t, err)
			require.Equal(t, 1, handler.count())
			assert.Equal(t, "mch_1", handler.received[0].MatchID)
			assert.Equal(t, apimatchmaking.EventTypeMatchMade, handler.received[0].EventType)
			require.Len(t, handler.received[0].Players, 2)
			assert.Equal(t, "p-1", handler.received[0].Players[0].PlayerID)
		})

		t.Run("不正なJSONのとき、エラーを返しマッチ成立イベントの処理は実行されない", func(t *testing.T) {
			handler := &fakeMatchHandler{}
			sub, err := NewMatchSubscriber(handler)
			require.NoError(t, err)

			err = sub.ProcessMessage(context.Background(), []byte("not-json"))

			assert.Error(t, err)
			assert.Zero(t, handler.count())
		})

		t.Run("未知のevent_typeのとき、エラーを返さず責務外としてマッチ成立イベントの処理は実行されない", func(t *testing.T) {
			handler := &fakeMatchHandler{}
			sub, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			payload, marshalErr := json.Marshal(apimatchmaking.MatchMadeEvent{
				EventType: "unknown",
				MatchID:   "mch_unk",
				Players:   validPlayers,
			})
			require.NoError(t, marshalErr)

			err = sub.ProcessMessage(context.Background(), payload)

			require.NoError(t, err)
			assert.Zero(t, handler.count())
		})

		t.Run("マッチ成立イベントの処理が失敗するとき、エラーを返し処理は実行される", func(t *testing.T) {
			handler := &fakeMatchHandler{err: errors.New("handler failed")}
			sub, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			data := wireBytes(t, apimatchmaking.MatchMadeEvent{MatchID: "mch_fail", Players: validPlayers})

			err = sub.ProcessMessage(context.Background(), data)

			assert.Error(t, err)
			assert.Equal(t, 1, handler.count(), "マッチ成立イベントの処理は実行された")
		})
	})
}
