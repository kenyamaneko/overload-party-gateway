package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// fakeAccountServer は FindByFirebaseUID / Register に実際の account サービスと
// 同じレスポンスを返す httptest.Server を起動する。
// PlayerResolve / DevAuthWithPlayerResolve は accountclient 経由で通信するため、
// テストにはモック型ではなくライブエンドポイントが必要。
type fakeAccountServer struct {
	mu      sync.Mutex
	players map[string]*accountclient.Player // firebaseUID → player
	server  *httptest.Server
}

func newFakeAccountServer() *fakeAccountServer {
	fa := &fakeAccountServer{players: map[string]*accountclient.Player{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/internal/v1/players/by-firebase-uid/", func(w http.ResponseWriter, r *http.Request) {
		uid := strings.TrimPrefix(r.URL.Path, "/internal/v1/players/by-firebase-uid/")
		fa.mu.Lock()
		defer fa.mu.Unlock()
		p, ok := fa.players[uid]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(p)
	})

	mux.HandleFunc("/internal/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			FirebaseUID string `json:"firebase_uid"`
			Username    string `json:"username"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fa.mu.Lock()
		defer fa.mu.Unlock()
		if _, ok := fa.players[body.FirebaseUID]; ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		p := &accountclient.Player{
			PlayerID:    "generated-" + body.FirebaseUID,
			FirebaseUID: body.FirebaseUID,
			Username:    body.Username,
		}
		fa.players[body.FirebaseUID] = p
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(p)
	})

	fa.server = httptest.NewServer(mux)
	return fa
}

func (f *fakeAccountServer) close() { f.server.Close() }

func (f *fakeAccountServer) client() *accountclient.Client {
	return accountclient.New(f.server.URL)
}

func (f *fakeAccountServer) seed(firebaseUID, playerID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.players[firebaseUID] = &accountclient.Player{
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
	fa := newFakeAccountServer()
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
	fa := newFakeAccountServer()
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
	fa := newFakeAccountServer()
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
	fa := newFakeAccountServer()
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
	fa := newFakeAccountServer()
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
