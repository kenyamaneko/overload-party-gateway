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

func (s *statefulAccountFake) close()                        { s.server.Close() }
func (s *statefulAccountFake) client() *accountclient.Client { return accountclient.New(s.server.URL()) }

func (s *statefulAccountFake) seed(firebaseUID, playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.players[firebaseUID] = apiaccount.PlayerResponse{
		PlayerID:    playerID,
		FirebaseUID: firebaseUID,
	}
}

func TestDevAuth(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantCode   int
	}{
		{"Success", "Bearer dev-token-user1", http.StatusOK},
		{"MissingHeader", "", http.StatusUnauthorized},
		{"NoBearerPrefix", "dev-token-user1", http.StatusUnauthorized},
		{"InvalidTokenFormat", "Bearer some-other-token", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(DevAuth())
			r.GET("/test", func(c *gin.Context) {
				uid := GetFirebaseUID(c)
				c.JSON(http.StatusOK, gin.H{"uid": uid})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, tt.wantCode, w.Code)

			if tt.wantCode == http.StatusOK {
				assert.Equal(t, `{"uid":"user1"}`, w.Body.String())
			}
		})
	}
}

func TestDevAuthWithPlayerResolve_AutoCreate(t *testing.T) {
	fa := newStatefulAccountFake()
	defer fa.close()

	setupCalled := false
	onCreated := DevPlayerSetup(func(ctx context.Context, playerID string) error {
		setupCalled = true
		return nil
	})

	r := gin.New()
	r.Use(DevAuthWithPlayerResolve(fa.client(), onCreated))
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
}

func TestDevAuthWithPlayerResolve_ExistingPlayer(t *testing.T) {
	fa := newStatefulAccountFake()
	defer fa.close()
	fa.seed("existinguser", "existing-id")

	r := gin.New()
	r.Use(DevAuthWithPlayerResolve(fa.client()))
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
}

func TestPlayerResolve_Success(t *testing.T) {
	fa := newStatefulAccountFake()
	defer fa.close()
	fa.seed("uid1", "p1")

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(firebaseUIDKey), "uid1")
		c.Next()
	})
	r.Use(PlayerResolve(fa.client()))
	r.GET("/test", func(c *gin.Context) {
		pid := GetPlayerID(c)
		c.JSON(http.StatusOK, gin.H{"player_id": pid})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, `{"player_id":"p1"}`, w.Body.String())
}

func TestPlayerResolve_MissingUID(t *testing.T) {
	fa := newStatefulAccountFake()
	defer fa.close()

	r := gin.New()
	r.Use(PlayerResolve(fa.client()))
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPlayerResolve_PlayerNotRegistered(t *testing.T) {
	fa := newStatefulAccountFake()
	defer fa.close()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(firebaseUIDKey), "unknown-uid")
		c.Next()
	})
	r.Use(PlayerResolve(fa.client()))
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetNotSet(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		wantBody string
	}{
		{"GetFirebaseUID_NotSet", "uid", `{"uid":""}`},
		{"GetPlayerID_NotSet", "pid", `{"pid":""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				var val string
				if tt.key == "uid" {
					val = GetFirebaseUID(c)
				} else {
					val = GetPlayerID(c)
				}
				c.JSON(http.StatusOK, gin.H{tt.key: val})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantBody, w.Body.String())
		})
	}
}
