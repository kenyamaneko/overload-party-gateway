package rest

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newPubSubPushEngine(processor *stubPushMessageProcessor) *gin.Engine {
	h := NewPubSubPushHandler(processor)
	r := gin.New()
	r.POST("/push", h.HandleMatchMade)
	return r
}

func doPubSubPush(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/push", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPubSubPushHandlerHandleMatchMade(t *testing.T) {
	t.Run("pushメッセージの受理", func(t *testing.T) {
		t.Run("リクエストボディが規定のpushメッセージ形式に沿っていないとき、ステータスコード400で、メッセージ形式が不正である旨のエラー内容を返す", func(t *testing.T) {
			cases := []struct {
				name string
				body string
			}{
				{
					name: "リクエストボディがJSONとして不正なとき、ステータスコード400で、メッセージ形式が不正である旨のエラー内容を返す",
					body: `{not-json`,
				},
				{
					name: "必須項目のmessageが欠けているとき、ステータスコード400で、メッセージ形式が不正である旨のエラー内容を返す",
					body: `{"subscription":"sub-1"}`,
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					processor := &stubPushMessageProcessor{}
					r := newPubSubPushEngine(processor)

					w := doPubSubPush(r, tc.body)

					assert.Equal(t, http.StatusBadRequest, w.Code)
					assert.Contains(t, w.Body.String(), "invalid push envelope")
					assert.Zero(t, processor.calls, "メッセージ形式が不正なときはメッセージ処理に到達しない")
				})
			}
		})

		t.Run("リクエストボディは規定の形式を満たすが、データ部分がBase64として正しくデコードできない値のとき、ステータスコード400で、データ形式が不正である旨のエラー内容を返す", func(t *testing.T) {
			processor := &stubPushMessageProcessor{}
			r := newPubSubPushEngine(processor)

			w := doPubSubPush(r, `{"message":{"data":"not-valid-base64!!","messageId":"m-1"}}`)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "invalid base64 data in message.data")
			assert.Zero(t, processor.calls, "データ部分をデコードできないときはメッセージ処理に到達しない")
		})

		t.Run("データ部分が正しくBase64デコードできても、その後のメッセージ処理自体が失敗したとき、ステータスコード500で、処理に失敗した旨のエラー内容を返す", func(t *testing.T) {
			processor := &stubPushMessageProcessor{err: errors.New("processing backend unavailable")}
			r := newPubSubPushEngine(processor)

			payload := base64.StdEncoding.EncodeToString([]byte(`{"match_id":"match-1"}`))
			w := doPubSubPush(r, `{"message":{"data":"`+payload+`","messageId":"m-2"}}`)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "failed to process message")
			assert.Equal(t, 1, processor.calls, "デコードには成功しているのでメッセージ処理へは到達する")
		})

		t.Run("データ部分が正しくBase64デコードでき、その後のメッセージ処理も成功したとき、ステータスコード200で、成功した旨の応答を返す", func(t *testing.T) {
			processor := &stubPushMessageProcessor{}
			r := newPubSubPushEngine(processor)

			payload := base64.StdEncoding.EncodeToString([]byte(`{"match_id":"match-2"}`))
			w := doPubSubPush(r, `{"message":{"data":"`+payload+`","messageId":"m-3"}}`)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
			assert.Equal(t, 1, processor.calls)
			assert.Equal(t, `{"match_id":"match-2"}`, string(processor.gotData))
		})
	})
}
