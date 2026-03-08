package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// DevAuth tests
// ---------------------------------------------------------------------------

func TestDevAuth_Success(t *testing.T) {
	r := gin.New()
	r.Use(DevAuth())
	r.GET("/test", func(c *gin.Context) {
		uid := GetFirebaseUID(c)
		c.JSON(http.StatusOK, gin.H{"uid": uid})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer dev-token-user1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != `{"uid":"user1"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestDevAuth_MissingHeader(t *testing.T) {
	r := gin.New()
	r.Use(DevAuth())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestDevAuth_NoBearerPrefix(t *testing.T) {
	r := gin.New()
	r.Use(DevAuth())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "dev-token-user1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestDevAuth_InvalidTokenFormat(t *testing.T) {
	r := gin.New()
	r.Use(DevAuth())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer some-other-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !registerCalled {
		t.Error("expected registerFn to be called")
	}
	if !setupCalled {
		t.Error("expected onCreated callback to be called")
	}
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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != `{"player_id":"existing-id"}` {
		t.Errorf("unexpected body: %s", body)
	}
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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
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

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
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

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// GetFirebaseUID / GetPlayerID edge cases
// ---------------------------------------------------------------------------

func TestGetFirebaseUID_NotSet(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		uid := GetFirebaseUID(c)
		c.JSON(http.StatusOK, gin.H{"uid": uid})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if body := w.Body.String(); body != `{"uid":""}` {
		t.Errorf("expected empty uid, got: %s", body)
	}
}

func TestGetPlayerID_NotSet(t *testing.T) {
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		pid := GetPlayerID(c)
		c.JSON(http.StatusOK, gin.H{"pid": pid})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if body := w.Body.String(); body != `{"pid":""}` {
		t.Errorf("expected empty pid, got: %s", body)
	}
}
