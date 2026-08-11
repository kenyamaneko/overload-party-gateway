package internalauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRouter(verifier Verifier, next gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(VerifyInternalAuth(verifier))
	router.GET("/", next)
	return router
}

func TestVerifyInternalAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("内部認証ヘッダー検証によるリクエストの合否判定", func(t *testing.T) {
		t.Run("X-Internal-Authヘッダが無い、またはヘッダの値が空文字列のとき、ヘッダが無い場合と同じ扱いで401Unauthorizedで拒否される", func(t *testing.T) {
			tests := []struct {
				name      string
				setHeader bool
			}{
				{name: "X-Internal-Authヘッダが無いとき", setHeader: false},
				{name: "X-Internal-Authヘッダの値が空文字列のとき", setHeader: true},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					var called bool
					verifier := &MockVerifier{VerifyFn: func(token string) (string, error) {
						return "unused-player-id", nil
					}}
					router := newTestRouter(verifier, func(c *gin.Context) {
						called = true
						c.Status(http.StatusOK)
					})

					req := httptest.NewRequest(http.MethodGet, "/", nil)
					if tt.setHeader {
						req.Header.Set("X-Internal-Auth", "")
					}
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)

					assert.Equal(t, http.StatusUnauthorized, w.Code)
					assert.False(t, called)

					var body map[string]string
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					assert.Equal(t, "X-Internal-Auth header is required", body["error"])
				})
			}
		})

		t.Run("ヘッダはあるが検証に失敗するとき、401Unauthorizedで拒否され、bodyのerrorフィールドに\"invalid internal auth token\"という内容が入り、後続の処理は実行されない", func(t *testing.T) {
			var called bool
			verifier := &MockVerifier{VerifyFn: func(token string) (string, error) {
				return "", errors.New("signature invalid")
			}}
			router := newTestRouter(verifier, func(c *gin.Context) {
				called = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Internal-Auth", "some-token")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.False(t, called)

			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "invalid internal auth token", body["error"])
		})

		t.Run("ヘッダがあり検証に成功するとき", func(t *testing.T) {
			const wantPlayerID = "player-001"
			const sentToken = "valid-internal-token"

			verifier := &MockVerifier{VerifyFn: func(token string) (string, error) {
				return wantPlayerID, nil
			}}

			var (
				called      bool
				playerIDVal any
				playerIDOk  bool
				tokenVal    any
				tokenValOk  bool
				ctxToken    string
				ctxTokenOk  bool
			)
			router := newTestRouter(verifier, func(c *gin.Context) {
				called = true
				playerIDVal, playerIDOk = c.Get("player_id")
				tokenVal, tokenValOk = c.Get("internal_auth_token")
				ctxToken, ctxTokenOk = TokenFrom(c.Request.Context())
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Internal-Auth", sentToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			t.Run("後続の処理に進む", func(t *testing.T) {
				assert.True(t, called)
			})

			t.Run("後続の処理から、キーplayer_idに対応する値として、検証で得られたプレイヤーIDを参照できるようになる", func(t *testing.T) {
				require.True(t, playerIDOk)
				assert.Equal(t, wantPlayerID, playerIDVal)
			})

			t.Run("後続の処理から、キーinternal_auth_tokenに対応する値として、受け取った内部認証トークン自体を参照できるようになる", func(t *testing.T) {
				require.True(t, tokenValOk)
				assert.Equal(t, sentToken, tokenVal)
			})

			t.Run("内部認証トークンは後続の下流サービスへの転送に使える形で、リクエストに紐づくコンテキストからも取り出せるようになる", func(t *testing.T) {
				require.True(t, ctxTokenOk)
				assert.Equal(t, sentToken, ctxToken)
			})
		})
	})
}
