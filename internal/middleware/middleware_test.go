package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newStatefulAccountFake は apiaccountserverfake を stateful な players map と
// 組み合わせて構成する helper。middleware テストは「FindByFirebaseUID → 404 なら
// Register → 以降の FindByFirebaseUID は見つかる」という遷移に依存するため、
// 固定 response ではなく map 経由の状態管理を Fn 側で閉じ込める。
//
// 返り値の seed は事前登録用。各テストの Arrange で呼ぶ。
type statefulAccountFake struct {
	server *apiaccountserverfake.Server
	mu     sync.Mutex
	// firebaseUID → Player
	players map[string]apiaccount.PlayerResponse
}

func newStatefulAccountFake() *statefulAccountFake {
	s := &statefulAccountFake{
		server:  apiaccountserverfake.NewServer(),
		players: map[string]apiaccount.PlayerResponse{},
	}
	s.server.FindByFirebaseUIDFn = func(firebaseUID string) (int, any) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if p, ok := s.players[firebaseUID]; ok {
			return http.StatusOK, p
		}
		return http.StatusNotFound, nil
	}
	s.server.RegisterFn = func(req apiaccount.RegisterRequest) (int, any) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, exists := s.players[req.FirebaseUID]; exists {
			return http.StatusConflict, nil
		}
		p := apiaccount.PlayerResponse{
			PlayerID:    "generated-" + req.FirebaseUID,
			FirebaseUID: req.FirebaseUID,
			// Register 時点では name 未確定 (オンボーディング完了で確定する契約)。
			Name: nil,
		}
		s.players[req.FirebaseUID] = p
		return http.StatusCreated, p
	}
	return s
}

func (s *statefulAccountFake) close() { s.server.Close() }
func (s *statefulAccountFake) client() *accountclient.Client {
	return accountclient.New(s.server.URL(), &http.Client{})
}

func (s *statefulAccountFake) seed(firebaseUID, playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.players[firebaseUID] = apiaccount.PlayerResponse{
		PlayerID:    playerID,
		FirebaseUID: firebaseUID,
	}
}

// newSequencedAccountFake は FindByFirebaseUIDFn の応答を呼び出し順に返し、
// RegisterFn は常に registerStatus を返す account フェイクを生成する。
func newSequencedAccountFake(t *testing.T, findStatuses []int, registerStatus int) *apiaccountserverfake.Server {
	t.Helper()
	fa := apiaccountserverfake.NewServer()
	t.Cleanup(fa.Close)
	callCount := 0
	fa.FindByFirebaseUIDFn = func(_ string) (int, any) {
		status := findStatuses[callCount]
		callCount++
		return status, apiaccount.PlayerResponse{PlayerID: "existing-id"}
	}
	fa.RegisterFn = func(_ apiaccount.RegisterRequest) (int, any) {
		return registerStatus, nil
	}
	return fa
}

func TestUseDevAuth(t *testing.T) {
	t.Run("[認証]開発用Bearer認証", func(t *testing.T) {
		t.Run("有効なdev-token-user1のとき、200でuidを解決する", func(t *testing.T) {
			r := gin.New()
			r.Use(UseDevAuth())
			r.GET("/test", func(c *gin.Context) {
				uid := GetFirebaseUID(c)
				c.JSON(http.StatusOK, gin.H{"uid": uid})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer dev-token-user1")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, `{"uid":"user1"}`, w.Body.String())
		})

		t.Run("Authorizationヘッダーが無いとき、401でmissing authorization headerを返す", func(t *testing.T) {
			r := gin.New()
			r.Use(UseDevAuth())
			r.GET("/test", func(c *gin.Context) {
				uid := GetFirebaseUID(c)
				c.JSON(http.StatusOK, gin.H{"uid": uid})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.JSONEq(t, `{"error":"missing authorization header"}`, w.Body.String())
		})

		invalidHeaderCases := []struct {
			name       string
			authHeader string
			wantBody   string
		}{
			{
				name:       "Bearer接頭辞が無いとき、401でinvalid authorization formatを返す",
				authHeader: "dev-token-user1",
				wantBody:   `{"error":"invalid authorization format"}`,
			},
			{
				name:       "dev-token形式でないtokenのとき、401でinvalid dev token formatを返す",
				authHeader: "Bearer some-other-token",
				wantBody:   `{"error":"invalid dev token format"}`,
			},
		}
		for _, tc := range invalidHeaderCases {
			t.Run(tc.name, func(t *testing.T) {
				r := gin.New()
				r.Use(UseDevAuth())
				r.GET("/test", func(c *gin.Context) {
					uid := GetFirebaseUID(c)
					c.JSON(http.StatusOK, gin.H{"uid": uid})
				})

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				require.Equal(t, http.StatusUnauthorized, w.Code)
				assert.JSONEq(t, tc.wantBody, w.Body.String())
			})
		}
	})
}

func TestUseDevAuthWithPlayerResolve(t *testing.T) {
	t.Run("[認証]開発用認証とプレイヤー解決", func(t *testing.T) {
		t.Run("未登録のプレイヤーのとき、プレイヤーを自動作成し作成後コールバックが呼ばれる", func(t *testing.T) {
			fa := newStatefulAccountFake()
			defer fa.close()

			setupCalled := false
			onCreated := DevPlayerSetup(func(ctx context.Context, playerID string) error {
				setupCalled = true
				return nil
			})

			r := gin.New()
			r.Use(UseDevAuthWithPlayerResolve(fa.client(), onCreated))
			r.GET("/test", func(c *gin.Context) {
				pid := GetPlayerID(c)
				c.JSON(http.StatusOK, gin.H{"player_id": pid})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer dev-token-newuser")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.True(t, setupCalled, "expected onCreated callback to be called")
			assert.Contains(t, w.Body.String(), "generated-newuser")
		})

		t.Run("既存のプレイヤーのとき、レスポンスに既存のプレイヤーIDが含まれる", func(t *testing.T) {
			fa := newStatefulAccountFake()
			defer fa.close()
			fa.seed("existinguser", "existing-id")

			r := gin.New()
			r.Use(UseDevAuthWithPlayerResolve(fa.client()))
			r.GET("/test", func(c *gin.Context) {
				pid := GetPlayerID(c)
				c.JSON(http.StatusOK, gin.H{"player_id": pid})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer dev-token-existinguser")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, `{"player_id":"existing-id"}`, w.Body.String())
		})

		authFormatCases := []struct {
			name       string
			authHeader string
		}{
			{name: "Authorizationヘッダーが無いとき、401になる", authHeader: ""},
			{name: "Bearer接頭辞が無いとき、401になる", authHeader: "dev-token-user1"},
			{name: "dev-token形式でないtokenのとき、401になる", authHeader: "Bearer other-token"},
		}
		for _, tc := range authFormatCases {
			t.Run(tc.name, func(t *testing.T) {
				fa := newSequencedAccountFake(t, nil, 0)

				r := gin.New()
				r.Use(UseDevAuthWithPlayerResolve(accountclient.New(fa.URL(), &http.Client{})))
				r.GET("/test", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"player_id": GetPlayerID(c)})
				})

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			})
		}

		resolveErrorCases := []struct {
			name             string
			authHeader       string
			findStatuses     []int
			registerStatus   int
			wantBodyContains string
		}{
			{
				name:             "プレイヤー検索が500のとき、500でfind by firebase uid:を含むエラーを返す",
				authHeader:       "Bearer dev-token-u-findfail",
				findStatuses:     []int{http.StatusInternalServerError},
				wantBodyContains: "resolve player: find by firebase uid:",
			},
			{
				name:             "未登録で登録が500のとき、500でregister dev player:を含むエラーを返す",
				authHeader:       "Bearer dev-token-u-registerfail",
				findStatuses:     []int{http.StatusNotFound},
				registerStatus:   http.StatusInternalServerError,
				wantBodyContains: "resolve player: register dev player:",
			},
			{
				name:             "登録が競合(409)し再検索も失敗するとき、500でrecover from race:を含むエラーを返す",
				authHeader:       "Bearer dev-token-u-racefail",
				findStatuses:     []int{http.StatusNotFound, http.StatusInternalServerError},
				registerStatus:   http.StatusConflict,
				wantBodyContains: "resolve player: recover from race:",
			},
			{
				name:             "登録が競合したのに再検索でも見つからないとき、500でregister returned already-registered but player not foundを返す",
				authHeader:       "Bearer dev-token-u-conflictmiss",
				findStatuses:     []int{http.StatusNotFound, http.StatusNotFound},
				registerStatus:   http.StatusConflict,
				wantBodyContains: "resolve player: register returned already-registered but player not found",
			},
		}
		for _, tc := range resolveErrorCases {
			t.Run(tc.name, func(t *testing.T) {
				fa := newSequencedAccountFake(t, tc.findStatuses, tc.registerStatus)

				r := gin.New()
				r.Use(UseDevAuthWithPlayerResolve(accountclient.New(fa.URL(), &http.Client{})))
				r.GET("/test", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"player_id": GetPlayerID(c)})
				})

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), tc.wantBodyContains)
			})
		}

		t.Run("登録が競合(409)し再検索で見つかるとき、レスポンスに既存のプレイヤーIDが含まれる", func(t *testing.T) {
			fa := newSequencedAccountFake(t, []int{http.StatusNotFound, http.StatusOK}, http.StatusConflict)

			r := gin.New()
			r.Use(UseDevAuthWithPlayerResolve(accountclient.New(fa.URL(), &http.Client{})))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"player_id": GetPlayerID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer dev-token-u-conflicthit")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, `{"player_id":"existing-id"}`, w.Body.String())
		})
	})
}

// stubTokenVerifier は port.TokenVerifier のテスト用実装。
type stubTokenVerifier struct {
	VerifyIDTokenFn func(ctx context.Context, idToken string) (string, error)
}

func (s *stubTokenVerifier) VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	return s.VerifyIDTokenFn(ctx, idToken)
}

func TestUseFirebaseAuth(t *testing.T) {
	t.Run("[認証]Firebase IDトークン認証", func(t *testing.T) {
		t.Run("検証に通るトークンのとき、200でUIDを解決する", func(t *testing.T) {
			verifier := &stubTokenVerifier{VerifyIDTokenFn: func(context.Context, string) (string, error) {
				return "firebase-uid-1", nil
			}}
			r := gin.New()
			r.Use(UseFirebaseAuth(verifier))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"uid": GetFirebaseUID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, `{"uid":"firebase-uid-1"}`, w.Body.String())
		})

		t.Run("検証に失敗するトークンのとき、401になる", func(t *testing.T) {
			verifier := &stubTokenVerifier{VerifyIDTokenFn: func(context.Context, string) (string, error) {
				return "", errors.New("invalid signature")
			}}
			r := gin.New()
			r.Use(UseFirebaseAuth(verifier))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"uid": GetFirebaseUID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer invalid-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, `{"error":"invalid token"}`, w.Body.String())
		})

		t.Run("Authorizationヘッダーが無いとき、401になる", func(t *testing.T) {
			verifier := &stubTokenVerifier{VerifyIDTokenFn: func(context.Context, string) (string, error) {
				return "firebase-uid-1", nil
			}}
			r := gin.New()
			r.Use(UseFirebaseAuth(verifier))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"uid": GetFirebaseUID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, `{"error":"missing authorization header"}`, w.Body.String())
		})

		t.Run("Bearer形式でないヘッダーのとき、401になる", func(t *testing.T) {
			verifier := &stubTokenVerifier{VerifyIDTokenFn: func(context.Context, string) (string, error) {
				return "firebase-uid-1", nil
			}}
			r := gin.New()
			r.Use(UseFirebaseAuth(verifier))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"uid": GetFirebaseUID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "some-id-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, `{"error":"invalid authorization format"}`, w.Body.String())
		})

		t.Run("Bearerの後が空のとき、401になる", func(t *testing.T) {
			verifier := &stubTokenVerifier{VerifyIDTokenFn: func(_ context.Context, idToken string) (string, error) {
				if idToken == "" {
					return "", errors.New("empty id token")
				}
				return "firebase-uid-1", nil
			}}
			r := gin.New()
			r.Use(UseFirebaseAuth(verifier))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"uid": GetFirebaseUID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer ")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, `{"error":"invalid token"}`, w.Body.String())
		})
	})
}

func TestUseFirebaseAuthWithPlayerResolve(t *testing.T) {
	t.Run("[認証]Firebase認証とプレイヤー解決のチェイン", func(t *testing.T) {
		t.Run("検証に通るトークンのとき、そのトークンで認証されたプレイヤーのIDがレスポンスに含まれる", func(t *testing.T) {
			fa := newStatefulAccountFake()
			defer fa.close()
			fa.seed("firebase-uid-1", "player-1")

			verifier := &stubTokenVerifier{VerifyIDTokenFn: func(context.Context, string) (string, error) {
				return "firebase-uid-1", nil
			}}

			r := gin.New()
			r.Use(UseFirebaseAuth(verifier))
			r.Use(ResolvePlayer(fa.client()))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"player_id": GetPlayerID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, `{"player_id":"player-1"}`, w.Body.String())
		})

		t.Run("トークンは有効だがプレイヤー未登録のとき、401になる", func(t *testing.T) {
			fa := newStatefulAccountFake()
			defer fa.close()
			// seed しないため FindByFirebaseUID は 404 (未登録) を返す。

			verifier := &stubTokenVerifier{VerifyIDTokenFn: func(context.Context, string) (string, error) {
				return "unregistered-uid", nil
			}}

			r := gin.New()
			r.Use(UseFirebaseAuth(verifier))
			r.Use(ResolvePlayer(fa.client()))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	})
}

func TestResolvePlayer(t *testing.T) {
	t.Run("[認証]firebase_uidからのプレイヤー解決", func(t *testing.T) {
		t.Run("firebase_uidが既存プレイヤーのとき、200になり後続のハンドラでそのプレイヤーのplayer_idを取得できる", func(t *testing.T) {
			fa := newStatefulAccountFake()
			defer fa.close()
			fa.seed("uid1", "p1")

			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(firebaseUIDKey), "uid1")
				c.Next()
			})
			r.Use(ResolvePlayer(fa.client()))
			r.GET("/test", func(c *gin.Context) {
				pid := GetPlayerID(c)
				c.JSON(http.StatusOK, gin.H{"player_id": pid})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, `{"player_id":"p1"}`, w.Body.String())
		})

		t.Run("firebase_uidが無いとき、401になる", func(t *testing.T) {
			fa := newStatefulAccountFake()
			defer fa.close()

			r := gin.New()
			r.Use(ResolvePlayer(fa.client()))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, `{"error":"missing firebase uid"}`, w.Body.String())
		})

		t.Run("firebase_uidが未登録のとき、401になる", func(t *testing.T) {
			fa := newStatefulAccountFake()
			defer fa.close()

			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(firebaseUIDKey), "unknown-uid")
				c.Next()
			})
			r.Use(ResolvePlayer(fa.client()))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Equal(t, `{"error":"player not registered"}`, w.Body.String())
		})

		t.Run("プレイヤー検索が500のとき、500になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			t.Cleanup(fa.Close)
			fa.FindByFirebaseUIDFn = func(_ string) (int, any) {
				return http.StatusInternalServerError, nil
			}

			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(firebaseUIDKey), "uid1")
				c.Next()
			})
			r.Use(ResolvePlayer(accountclient.New(fa.URL(), &http.Client{})))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Equal(t, `{"error":"failed to resolve player"}`, w.Body.String())
		})
	})
}

func TestContextGetters(t *testing.T) {
	t.Run("[認証]context値の取得", func(t *testing.T) {
		t.Run("firebase_uidが未設定のとき、ハンドラが取得するfirebase_uidは空文字になる", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"uid": GetFirebaseUID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, `{"uid":""}`, w.Body.String())
		})

		t.Run("player_idが未設定のとき、ハンドラが取得するplayer_idは空文字になる", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"pid": GetPlayerID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, `{"pid":""}`, w.Body.String())
		})
	})
}
