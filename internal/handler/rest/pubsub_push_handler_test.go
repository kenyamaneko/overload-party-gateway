package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePushProcessor は port.PushMessageProcessor のテスト用実装。
type fakePushProcessor struct {
	err   error
	calls [][]byte
}

func (f *fakePushProcessor) ProcessMessage(_ context.Context, data []byte) error {
	f.calls = append(f.calls, data)
	return f.err
}

// newPushTestRouter は PubSubPushHandler.HandleMatchMade だけを登録したルーターを返す。
func newPushTestRouter(processor *fakePushProcessor) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewPubSubPushHandler(processor)
	r.POST("/internal/v1/pubsub/match-made", h.HandleMatchMade)
	return r
}

func postPush(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/pubsub/match-made", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestHandleMatchMade(t *testing.T) {
	t.Run("push 配信エンドポイント", func(t *testing.T) {
		t.Run("有効な envelope のとき、200 を返し復号したペイロードを処理へ渡す", func(t *testing.T) {
			processor := &fakePushProcessor{}
			r := newPushTestRouter(processor)
			payload := base64.StdEncoding.EncodeToString([]byte(`{"event_type":"match_made"}`))
			body := `{"message":{"data":"` + payload + `","messageId":"msg-1"},"subscription":"projects/p/subscriptions/s"}`

			w := postPush(r, body)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "ok", decodeBody(t, w)["status"])
			require.Len(t, processor.calls, 1)
			assert.Equal(t, `{"event_type":"match_made"}`, string(processor.calls[0]))
		})

		t.Run("JSON として不正な本文のとき、400 とレスポンスボディに invalid push envelope が返る", func(t *testing.T) {
			processor := &fakePushProcessor{}
			r := newPushTestRouter(processor)

			w := postPush(r, `not-json`)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid push envelope", decodeBody(t, w)["error"])
			assert.Empty(t, processor.calls)
		})

		t.Run("message.data が欠けている envelope のとき、400 とレスポンスボディに invalid push envelope が返る", func(t *testing.T) {
			processor := &fakePushProcessor{}
			r := newPushTestRouter(processor)

			w := postPush(r, `{"message":{"messageId":"msg-1"}}`)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid push envelope", decodeBody(t, w)["error"])
			assert.Empty(t, processor.calls)
		})

		t.Run("message フィールド自体が無い envelope のとき、400 とレスポンスボディに invalid push envelope が返る", func(t *testing.T) {
			processor := &fakePushProcessor{}
			r := newPushTestRouter(processor)

			w := postPush(r, `{"subscription":"projects/p/subscriptions/s"}`)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid push envelope", decodeBody(t, w)["error"])
			assert.Empty(t, processor.calls)
		})

		t.Run("message.data が空文字のとき、400 とレスポンスボディに invalid push envelope が返る", func(t *testing.T) {
			processor := &fakePushProcessor{}
			r := newPushTestRouter(processor)

			w := postPush(r, `{"message":{"data":"","messageId":"msg-1"}}`)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid push envelope", decodeBody(t, w)["error"])
			assert.Empty(t, processor.calls)
		})

		t.Run("message フィールドの中身が空のとき、400 とレスポンスボディに invalid push envelope が返る", func(t *testing.T) {
			processor := &fakePushProcessor{}
			r := newPushTestRouter(processor)

			w := postPush(r, `{"message":{}}`)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid push envelope", decodeBody(t, w)["error"])
			assert.Empty(t, processor.calls)
		})

		t.Run("message.data が base64 として不正なとき、400 とレスポンスボディに invalid base64 data が返る", func(t *testing.T) {
			processor := &fakePushProcessor{}
			r := newPushTestRouter(processor)

			w := postPush(r, `{"message":{"data":"not-valid-base64!!","messageId":"msg-1"}}`)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Equal(t, "invalid base64 data in message.data", decodeBody(t, w)["error"])
			assert.Empty(t, processor.calls, "base64 復号に失敗した時点で処理へ渡してはならない")
		})

		t.Run("処理が失敗するとき、500 とレスポンスボディに failed to process message が返る", func(t *testing.T) {
			processor := &fakePushProcessor{err: errors.New("dedup handler failed")}
			r := newPushTestRouter(processor)
			payload := base64.StdEncoding.EncodeToString([]byte(`{"event_type":"match_made"}`))
			body := `{"message":{"data":"` + payload + `","messageId":"msg-1"}}`

			w := postPush(r, body)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Equal(t, "failed to process message", decodeBody(t, w)["error"])
			assert.Len(t, processor.calls, 1, "処理は試みた上で失敗した")
		})
	})
}
