package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
)

func TestUseFirebaseAuth(t *testing.T) {
	t.Run("[認証]Firebase IDトークンによるリクエスト認証", func(t *testing.T) {
		t.Run("リクエストに認証ヘッダーが無いとき、401で拒否される", func(t *testing.T) {
			reached := false
			var gotUID string
			r := gin.New()
			r.GET("/test", UseFirebaseAuth(&stubTokenVerifier{}), func(c *gin.Context) {
				reached = true
				gotUID = GetFirebaseUID(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing authorization header")
			assert.False(t, reached)
			assert.Equal(t, "", gotUID)
		})

		cases := []struct {
			name       string
			authHeader string
			verifier   *stubTokenVerifier
			wantBody   string
		}{
			{
				name:       `認証ヘッダーが"Bearer "で始まっていないとき、401で拒否される(不正な形式)`,
				authHeader: "Token abc123",
				verifier:   &stubTokenVerifier{},
				wantBody:   "invalid authorization format",
			},
			{
				name:       `認証ヘッダーが"Bearer "のみでトークン本体が空のとき、401で拒否される`,
				authHeader: "Bearer ",
				verifier: &stubTokenVerifier{
					verifyIDToken: func(ctx context.Context, idToken string) (string, error) {
						return "", errors.New("empty token rejected")
					},
				},
				wantBody: "invalid token",
			},
			{
				name:       "トークンが検証者によって無効と判定されたとき、401で拒否される",
				authHeader: "Bearer invalid-token",
				verifier: &stubTokenVerifier{
					verifyIDToken: func(ctx context.Context, idToken string) (string, error) {
						return "", errors.New("token verification failed")
					},
				},
				wantBody: "invalid token",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				var gotUID string
				r := gin.New()
				r.GET("/test", UseFirebaseAuth(tc.verifier), func(c *gin.Context) {
					reached = true
					gotUID = GetFirebaseUID(c)
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusUnauthorized, w.Code)
				assert.Contains(t, w.Body.String(), tc.wantBody)
				assert.False(t, reached)
				assert.Equal(t, "", gotUID)
			})
		}

		t.Run("トークンが有効と判定されたとき、リクエストは後続の処理に進む", func(t *testing.T) {
			reached := false
			var gotUID string
			verifier := &stubTokenVerifier{
				verifyIDToken: func(ctx context.Context, idToken string) (string, error) {
					return "firebase-uid-1", nil
				},
			}
			r := gin.New()
			r.GET("/test", UseFirebaseAuth(verifier), func(c *gin.Context) {
				reached = true
				gotUID = GetFirebaseUID(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, reached)
			assert.Equal(t, "firebase-uid-1", gotUID)
		})
	})
}

func TestResolvePlayer(t *testing.T) {
	t.Run("[認証]認証済みユーザーのプレイヤー解決", func(t *testing.T) {
		verifierReturning := func(uid string) *stubTokenVerifier {
			return &stubTokenVerifier{
				verifyIDToken: func(ctx context.Context, idToken string) (string, error) {
					return uid, nil
				},
			}
		}

		cases := []struct {
			name          string
			verifier      *stubTokenVerifier
			accountClient *stubAccountClient
			wantStatus    int
			wantBody      string
			wantReached   bool
			wantPlayerID  string
		}{
			{
				name:          "「Firebase IDトークンによるリクエスト認証」を通過した認証結果にユーザー識別子が含まれていないとき、401で拒否される",
				verifier:      verifierReturning(""),
				accountClient: &stubAccountClient{},
				wantStatus:    http.StatusUnauthorized,
				wantBody:      "missing firebase uid",
			},
			{
				name:     "ユーザーに対応するプレイヤー情報の照会自体が失敗したとき(存在しないという結果ではなく、照会処理そのものが失敗したとき)、500で拒否される",
				verifier: verifierReturning("firebase-uid-1"),
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, errors.New("account service unavailable")
					},
				},
				wantStatus: http.StatusInternalServerError,
				wantBody:   "failed to resolve player",
			},
			{
				name:     "ユーザーに対応する登録済みプレイヤーが見つからないとき、401で拒否される(未登録)",
				verifier: verifierReturning("firebase-uid-1"),
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, nil
					},
				},
				wantStatus: http.StatusUnauthorized,
				wantBody:   "player not registered",
			},
			{
				name:     "ユーザーに対応する登録済みプレイヤーが見つかったとき、そのプレイヤーとしてリクエストは後続の処理に進む",
				verifier: verifierReturning("firebase-uid-1"),
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return &apiaccount.PlayerResponse{PlayerID: "player-1"}, nil
					},
				},
				wantStatus:   http.StatusOK,
				wantReached:  true,
				wantPlayerID: "player-1",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				var gotPlayerID string
				r := gin.New()
				r.GET("/test", UseFirebaseAuth(tc.verifier), ResolvePlayer(tc.accountClient), func(c *gin.Context) {
					reached = true
					gotPlayerID = GetPlayerID(c)
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer some-token")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, tc.wantStatus, w.Code)
				assert.Contains(t, w.Body.String(), tc.wantBody)
				assert.Equal(t, tc.wantReached, reached)
				assert.Equal(t, tc.wantPlayerID, gotPlayerID)
			})
		}
	})
}
