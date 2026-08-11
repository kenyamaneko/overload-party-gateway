package pubsub

import (
	"context"
	"testing"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMatchEventHandler struct {
	err           error
	handledCount  int
	receivedEvent apimatchmaking.MatchMadeEvent
}

func (s *stubMatchEventHandler) HandleMatchMade(ctx context.Context, event apimatchmaking.MatchMadeEvent) error {
	s.handledCount++
	s.receivedEvent = event
	return s.err
}

func TestNewMatchSubscriber(t *testing.T) {
	t.Run("[PubSub連携]イベントディスパッチャの生成", func(t *testing.T) {
		t.Run("イベントの配信先が指定されているとき、生成は成功する", func(t *testing.T) {
			handler := &stubMatchEventHandler{}

			subscriber, err := NewMatchSubscriber(handler)

			require.NoError(t, err)
			require.NotNil(t, subscriber)
		})

		t.Run("イベントの配信先が指定されていない(nil)とき、生成はエラーになる", func(t *testing.T) {
			_, err := NewMatchSubscriber(nil)

			require.Error(t, err)
		})
	})
}

func TestProcessMessage(t *testing.T) {
	t.Run("[PubSub連携]match_madeイベント通知の処理", func(t *testing.T) {
		t.Run("受信したデータが正しい形式で、対象のイベント種別(match_made)であり、配信先での処理が成功するとき、通知処理はエラーにならず完了する", func(t *testing.T) {
			handler := &stubMatchEventHandler{}
			subscriber, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			data := []byte(`{"event_type":"match_made","match_id":"match-1","players":[{"player_id":"player-1","deck_id":42,"name":"dummy-player","level":7}]}`)

			err = subscriber.ProcessMessage(context.Background(), data)

			require.NoError(t, err)
			require.Equal(t, 1, handler.handledCount)
			assert.Equal(t, "match-1", handler.receivedEvent.MatchID)
			require.Len(t, handler.receivedEvent.Players, 1)
			assert.Equal(t, apimatchmaking.MatchedPlayer{
				PlayerID: "player-1",
				DeckID:   42,
				Name:     "dummy-player",
				Level:    7,
			}, handler.receivedEvent.Players[0])
		})

		t.Run("受信したデータが正しい形式で、対象のイベント種別(match_made)だが、配信先での処理が失敗するとき、通知処理はエラーになる", func(t *testing.T) {
			handler := &stubMatchEventHandler{err: assert.AnError}
			subscriber, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			data := []byte(`{"event_type":"match_made","match_id":"match-1","players":[]}`)

			err = subscriber.ProcessMessage(context.Background(), data)

			require.ErrorIs(t, err, assert.AnError)
		})

		t.Run("受信したデータが定められた形式として解釈できない(不正な形式)とき、通知処理はエラーになり、配信先へは何も渡されない", func(t *testing.T) {
			handler := &stubMatchEventHandler{}
			subscriber, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			data := []byte(`not-json`)

			err = subscriber.ProcessMessage(context.Background(), data)

			require.Error(t, err)
			assert.Equal(t, 0, handler.handledCount)
		})

		t.Run("受信したデータが空(0バイト)のとき、定められた形式として解釈できないため、通知処理はエラーになる", func(t *testing.T) {
			handler := &stubMatchEventHandler{}
			subscriber, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			data := []byte{}

			err = subscriber.ProcessMessage(context.Background(), data)

			require.Error(t, err)
		})

		t.Run("受信したデータの形式は正しいが、イベント種別が対象外(match_made以外)のとき、通知処理はエラーにならず、配信先へは何も渡されない", func(t *testing.T) {
			handler := &stubMatchEventHandler{}
			subscriber, err := NewMatchSubscriber(handler)
			require.NoError(t, err)
			data := []byte(`{"event_type":"match_abandoned","match_id":"match-1","players":[]}`)

			err = subscriber.ProcessMessage(context.Background(), data)

			require.NoError(t, err)
			assert.Equal(t, 0, handler.handledCount)
		})
	})
}
