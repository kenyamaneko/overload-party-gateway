package middleware

import (
	"context"
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
	return accountclient.New(s.server.URL())
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
	t.Run("開発用 Bearer 認証", func(t *testing.T) {
		t.Run("有効な dev-token-user1 のとき、200 で uid を解決する", func(t *testing.T) {
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

		t.Run("Authorization header が無いとき、401 になる", func(t *testing.T) {
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
		})

		invalidHeaderCases := []struct {
			name       string
			authHeader string
		}{
			{name: "Bearer 接頭辞が無いとき、401 になる", authHeader: "dev-token-user1"},
			{name: "dev-token 形式でない token のとき、401 になる", authHeader: "Bearer some-other-token"},
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
			})
		}
	})
}

func TestUseDevAuthWithPlayerResolve(t *testing.T) {
	t.Run("開発用認証とプレイヤー解決", func(t *testing.T) {
		t.Run("未登録ユーザーのとき、自動作成し onCreated が呼ばれる", func(t *testing.T) {
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

		t.Run("既存ユーザーのとき、既存 player_id を解決する", func(t *testing.T) {
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

		statusCases := []struct {
			name           string
			authHeader     string
			findStatuses   []int
			registerStatus int
			wantStatus     int
		}{
			{
				name:       "Authorization header が無いとき、401 になる",
				authHeader: "",
				wantStatus: http.StatusUnauthorized,
			},
			{
				name:       "Bearer 接頭辞が無いとき、401 になる",
				authHeader: "dev-token-user1",
				wantStatus: http.StatusUnauthorized,
			},
			{
				name:       "dev-token 形式でない token のとき、401 になる",
				authHeader: "Bearer other-token",
				wantStatus: http.StatusUnauthorized,
			},
			{
				name:         "プレイヤー検索が 500 のとき、500 になる",
				authHeader:   "Bearer dev-token-u500",
				findStatuses: []int{http.StatusInternalServerError},
				wantStatus:   http.StatusInternalServerError,
			},
			{
				name:           "未登録で登録が 500 のとき、500 になる",
				authHeader:     "Bearer dev-token-u-registerfail",
				findStatuses:   []int{http.StatusNotFound},
				registerStatus: http.StatusInternalServerError,
				wantStatus:     http.StatusInternalServerError,
			},
			{
				name:           "登録が競合したのに再検索でも見つからないとき、500 になる",
				authHeader:     "Bearer dev-token-u-conflictmiss",
				findStatuses:   []int{http.StatusNotFound, http.StatusNotFound},
				registerStatus: http.StatusConflict,
				wantStatus:     http.StatusInternalServerError,
			},
		}
		for _, tc := range statusCases {
			t.Run(tc.name, func(t *testing.T) {
				fa := newSequencedAccountFake(t, tc.findStatuses, tc.registerStatus)

				r := gin.New()
				r.Use(UseDevAuthWithPlayerResolve(accountclient.New(fa.URL())))
				r.GET("/test", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"player_id": GetPlayerID(c)})
				})

				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("Authorization", tc.authHeader)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			})
		}

		t.Run("登録が競合 (409) し再検索で見つかるとき、既存のプレイヤー ID で通る", func(t *testing.T) {
			fa := newSequencedAccountFake(t, []int{http.StatusNotFound, http.StatusOK}, http.StatusConflict)

			r := gin.New()
			r.Use(UseDevAuthWithPlayerResolve(accountclient.New(fa.URL())))
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

func TestResolvePlayer(t *testing.T) {
	t.Run("firebase_uid からのプレイヤー解決", func(t *testing.T) {
		t.Run("firebase_uid が既存プレイヤーのとき、player_id を解決する", func(t *testing.T) {
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

		t.Run("firebase_uid が無いとき、401 になる", func(t *testing.T) {
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
		})

		t.Run("firebase_uid が未登録のとき、401 になる", func(t *testing.T) {
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
		})

		t.Run("プレイヤー検索が 500 のとき、500 になる", func(t *testing.T) {
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
			r.Use(ResolvePlayer(accountclient.New(fa.URL())))
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
		})
	})
}

func TestContextGetters(t *testing.T) {
	t.Run("未設定時の context getter", func(t *testing.T) {
		t.Run("GetFirebaseUID は未設定のとき、空文字を返す", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"uid": GetFirebaseUID(c)})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, `{"uid":""}`, w.Body.String())
		})

		t.Run("GetPlayerID は未設定のとき、空文字を返す", func(t *testing.T) {
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
