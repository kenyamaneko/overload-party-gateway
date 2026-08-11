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
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

func TestUseDevAuth(t *testing.T) {
	t.Run("[認証]開発用トークンによるリクエスト認証", func(t *testing.T) {
		t.Run("リクエストに認証ヘッダーが無いとき、401で拒否される", func(t *testing.T) {
			reached := false
			var gotUID string
			r := gin.New()
			r.GET("/test", UseDevAuth(), func(c *gin.Context) {
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

		formatCases := []struct {
			name       string
			authHeader string
			wantBody   string
		}{
			{
				name:       `認証ヘッダーが"Bearer "で始まっていないとき、401で拒否される(不正な形式)`,
				authHeader: "Token abc123",
				wantBody:   "invalid authorization format",
			},
			{
				name:       "トークン本体が開発用トークンの接頭辞で始まっていないとき、401で拒否される(不正な開発用トークン形式)",
				authHeader: "Bearer not-a-dev-token",
				wantBody:   "invalid dev token format",
			},
		}

		for _, tc := range formatCases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				var gotUID string
				r := gin.New()
				r.GET("/test", UseDevAuth(), func(c *gin.Context) {
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

		successCases := []struct {
			name       string
			authHeader string
			wantUID    string
		}{
			{
				name:       "トークン本体が接頭辞のみでユーザー識別子部分が空のとき、識別子が空文字のまま認証は成功し後続の処理に進む",
				authHeader: "Bearer " + DevTokenPrefix,
				wantUID:    "",
			},
			{
				name:       "トークン本体が接頭辞に続くユーザー識別子を持つとき、そのユーザー識別子で認証が成功し後続の処理に進む",
				authHeader: "Bearer " + DevTokenPrefix + "user-42",
				wantUID:    "user-42",
			},
		}

		for _, tc := range successCases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				var gotUID string
				r := gin.New()
				r.GET("/test", UseDevAuth(), func(c *gin.Context) {
					reached = true
					gotUID = GetFirebaseUID(c)
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusOK, w.Code)
				assert.True(t, reached)
				assert.Equal(t, tc.wantUID, gotUID)
			})
		}
	})
}

func TestUseDevAuthWithPlayerResolve(t *testing.T) {
	t.Run("[認証]開発用プレイヤーの解決と自動登録", func(t *testing.T) {
		t.Run("リクエストに認証ヘッダーが無いとき、401で拒否される", func(t *testing.T) {
			reached := false
			var gotPlayerID string
			r := gin.New()
			r.GET("/test", UseDevAuthWithPlayerResolve(&stubAccountClient{}), func(c *gin.Context) {
				reached = true
				gotPlayerID = GetPlayerID(c)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing authorization header")
			assert.False(t, reached)
			assert.Equal(t, "", gotPlayerID)
		})

		formatCases := []struct {
			name       string
			authHeader string
			wantBody   string
		}{
			{
				name:       `認証ヘッダーが"Bearer "で始まっていないとき、401で拒否される(不正な形式)`,
				authHeader: "Token abc123",
				wantBody:   "invalid authorization format",
			},
			{
				name:       "トークン本体が開発用トークンの接頭辞で始まっていないとき、401で拒否される(不正な開発用トークン形式)",
				authHeader: "Bearer not-a-dev-token",
				wantBody:   "invalid dev token format",
			},
		}

		for _, tc := range formatCases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				var gotPlayerID string
				r := gin.New()
				r.GET("/test", UseDevAuthWithPlayerResolve(&stubAccountClient{}), func(c *gin.Context) {
					reached = true
					gotPlayerID = GetPlayerID(c)
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusUnauthorized, w.Code)
				assert.Contains(t, w.Body.String(), tc.wantBody)
				assert.False(t, reached)
				assert.Equal(t, "", gotPlayerID)
			})
		}

		successCases := []struct {
			name          string
			accountClient *stubAccountClient
			wantPlayerID  string
		}{
			{
				name: "ユーザー識別子に対応する登録済みプレイヤーが既に存在するとき、そのプレイヤーとして後続の処理に進む",
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return &apiaccount.PlayerResponse{PlayerID: "existing-player-1"}, nil
					},
				},
				wantPlayerID: "existing-player-1",
			},
			{
				name: "ユーザー識別子に対応する登録済みプレイヤーが存在せず、新規登録が成功したとき、新しいプレイヤーとして後続の処理に進む",
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, nil
					},
					register: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return &apiaccount.PlayerResponse{PlayerID: "new-player-1"}, nil
					},
				},
				wantPlayerID: "new-player-1",
			},
			{
				name:          "新規登録の最中に同時に別のリクエストが同じユーザーを登録済みにしていた場合(競合)、再照会によって既存のプレイヤーが見つかれば、そのプレイヤーとして後続の処理に進む",
				accountClient: newConflictThenAccountClient(&apiaccount.PlayerResponse{PlayerID: "concurrent-player-1"}, nil),
				wantPlayerID:  "concurrent-player-1",
			},
		}

		for _, tc := range successCases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				var gotPlayerID string
				r := gin.New()
				r.GET("/test", UseDevAuthWithPlayerResolve(tc.accountClient), func(c *gin.Context) {
					reached = true
					gotPlayerID = GetPlayerID(c)
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer "+DevTokenPrefix+"user-1")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusOK, w.Code)
				assert.True(t, reached)
				assert.Equal(t, tc.wantPlayerID, gotPlayerID)
			})
		}

		resolveErrorCases := []struct {
			name          string
			accountClient *stubAccountClient
			wantBody      string
		}{
			{
				name: "ユーザー識別子に対応するプレイヤーの照会自体が失敗したとき、500で拒否される",
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, errors.New("account backend unreachable")
					},
				},
				wantBody: "find by firebase uid",
			},
			{
				name:          "競合による再照会自体が失敗したとき、500で拒否される",
				accountClient: newConflictThenAccountClient(nil, errors.New("account backend unreachable on refind")),
				wantBody:      "recover from race",
			},
			{
				name: "競合が報告されたにもかかわらず再照会でプレイヤーが見つからないとき、500で拒否される",
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, nil
					},
					register: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, port.ErrPlayerAlreadyRegistered
					},
				},
				wantBody: "already-registered but player not found",
			},
			{
				name: "新規登録が競合以外の理由で失敗したとき、500で拒否される",
				accountClient: &stubAccountClient{
					findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, nil
					},
					register: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
						return nil, errors.New("register failed hard")
					},
				},
				wantBody: "register dev player",
			},
		}

		for _, tc := range resolveErrorCases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				var gotPlayerID string
				r := gin.New()
				r.GET("/test", UseDevAuthWithPlayerResolve(tc.accountClient), func(c *gin.Context) {
					reached = true
					gotPlayerID = GetPlayerID(c)
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer "+DevTokenPrefix+"user-1")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusInternalServerError, w.Code)
				assert.Contains(t, w.Body.String(), tc.wantBody)
				assert.False(t, reached)
				assert.Equal(t, "", gotPlayerID)
			})
		}
	})
}

func newConflictThenAccountClient(refindPlayer *apiaccount.PlayerResponse, refindErr error) *stubAccountClient {
	findCalls := 0
	return &stubAccountClient{
		findByFirebaseUID: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
			findCalls++
			if findCalls == 1 {
				return nil, nil
			}
			return refindPlayer, refindErr
		},
		register: func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
			return nil, port.ErrPlayerAlreadyRegistered
		},
	}
}
