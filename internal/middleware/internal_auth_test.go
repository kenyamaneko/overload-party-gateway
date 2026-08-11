package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func TestIssueInternalAuth(t *testing.T) {
	t.Run("内部認証トークンの発行", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		workingSigner := internalauth.NewSigner(internalauth.StaticPrivateKeyResolver(key, internalauth.DefaultKeyID), internalauth.DefaultKeyID)
		failingSigner := internalauth.NewSigner(func(internalauth.KeyID) (*rsa.PrivateKey, error) {
			return nil, errors.New("signing key not found")
		}, internalauth.DefaultKeyID)

		t.Run("リクエストにプレイヤー識別子が設定されていないとき、401で拒否される", func(t *testing.T) {
			reached := false
			r := gin.New()
			r.GET("/test", IssueInternalAuth(workingSigner), func(c *gin.Context) {
				reached = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing player id")
			assert.False(t, reached)
		})

		t.Run("リクエストにプレイヤー識別子が空文字のとき、401で拒否される", func(t *testing.T) {
			reached := false
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				c.Set(string(playerIDKey), "")
				c.Next()
			}, IssueInternalAuth(workingSigner), func(c *gin.Context) {
				reached = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing player id")
			assert.False(t, reached)
		})

		t.Run("プレイヤー識別子は設定されているが、トークンの発行自体が失敗したとき、500で拒否される", func(t *testing.T) {
			reached := false
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				c.Set(string(playerIDKey), "player-1")
				c.Next()
			}, IssueInternalAuth(failingSigner), func(c *gin.Context) {
				reached = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "failed to issue internal auth token")
			assert.False(t, reached)
		})

		t.Run("プレイヤー識別子が設定されており、トークンの発行が成功したとき、そのプレイヤー識別子をsubクレームに含むトークンを伴って後続の処理に進む", func(t *testing.T) {
			const playerID = "player-1"
			reached := false
			var gotToken string
			var gotOK bool
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				c.Set(string(playerIDKey), playerID)
				c.Next()
			}, IssueInternalAuth(workingSigner), func(c *gin.Context) {
				reached = true
				gotToken, gotOK = internalauth.TokenFrom(c.Request.Context())
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, reached)
			require.True(t, gotOK)

			claims := &jwt.RegisteredClaims{}
			_, err := jwt.ParseWithClaims(gotToken, claims, func(*jwt.Token) (interface{}, error) {
				return &key.PublicKey, nil
			})
			require.NoError(t, err)
			assert.Equal(t, playerID, claims.Subject)
		})
	})
}
