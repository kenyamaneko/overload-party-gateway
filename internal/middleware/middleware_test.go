package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// DevAuth tests
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// DevAuthWithPlayerResolve tests
// ---------------------------------------------------------------------------

func TestDevAuthWithPlayerResolve_AutoCreate(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()

	registerCalled := false
	registerFn := func(ctx context.Context, firebaseUID, username string) (string, error) {
		registerCalled = true
		// Simulate registration
		p := &model.Player{
			PlayerID:    "generated-id",
			FirebaseUID: firebaseUID,
			Username:    username,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		_ = playerRepo.Create(context.TODO(), p, &model.PlayerDailyBattle{PlayerID: p.PlayerID})
		return p.PlayerID, nil
	}

	setupCalled := false
	onCreated := DevPlayerSetup(func(ctx context.Context, playerID string) error {
		setupCalled = true
		return nil
	})

	r := gin.New()
	r.Use(DevAuthWithPlayerResolve(playerRepo, registerFn, onCreated))
	r.GET("/test", func(c *gin.Context) {
		pid := GetPlayerID(c)
		c.JSON(http.StatusOK, gin.H{"player_id": pid})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer dev-token-newuser")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, registerCalled, "expected registerFn to be called")
	assert.True(t, setupCalled, "expected onCreated callback to be called")
}

func TestDevAuthWithPlayerResolve_ExistingPlayer(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	_ = playerRepo.Create(context.TODO(), &model.Player{
		PlayerID:    "existing-id",
		FirebaseUID: "existinguser",
		Username:    "Existing",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, &model.PlayerDailyBattle{PlayerID: "existing-id"})

	registerFn := func(ctx context.Context, firebaseUID, username string) (string, error) {
		t.Fatal("registerFn should not be called for existing player")
		return "", nil
	}

	r := gin.New()
	r.Use(DevAuthWithPlayerResolve(playerRepo, registerFn))
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

// ---------------------------------------------------------------------------
// PlayerResolve tests
// ---------------------------------------------------------------------------

func TestPlayerResolve_Success(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()
	_ = playerRepo.Create(context.TODO(), &model.Player{
		PlayerID:    "p1",
		FirebaseUID: "uid1",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, &model.PlayerDailyBattle{PlayerID: "p1"})

	r := gin.New()
	// Simulate FirebaseAuth having set firebase_uid
	r.Use(func(c *gin.Context) {
		c.Set(string(firebaseUIDKey), "uid1")
		c.Next()
	})
	r.Use(PlayerResolve(playerRepo))
	r.GET("/test", func(c *gin.Context) {
		pid := GetPlayerID(c)
		c.JSON(http.StatusOK, gin.H{"player_id": pid})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestPlayerResolve_MissingUID(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()

	r := gin.New()
	// No firebase_uid set
	r.Use(PlayerResolve(playerRepo))
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPlayerResolve_PlayerNotRegistered(t *testing.T) {
	playerRepo := repository.NewMockPlayerRepository()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(firebaseUIDKey), "unknown-uid")
		c.Next()
	})
	r.Use(PlayerResolve(playerRepo))
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ---------------------------------------------------------------------------
// GetFirebaseUID / GetPlayerID edge cases
// ---------------------------------------------------------------------------

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
