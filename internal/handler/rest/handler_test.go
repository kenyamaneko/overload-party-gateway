package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setPlayerID is a test middleware that sets player_id in context.
func setPlayerID(playerID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("player_id", playerID)
		c.Set("firebase_uid", "uid-"+playerID)
		c.Next()
	}
}

// ---------------------------------------------------------------------------
// AuthHandler tests
// ---------------------------------------------------------------------------

func setupAuthRouter() (*gin.Engine, *repository.MockPlayerRepository) {
	playerRepo := repository.NewMockPlayerRepository()
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo)
	handler := NewAuthHandler(authService)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.POST("/auth/register", handler.Register)
	r.POST("/auth/login", handler.Login)
	return r, playerRepo
}

func TestAuthHandler_Register_Success(t *testing.T) {
	r, _ := setupAuthRouter()

	body, _ := json.Marshal(map[string]string{"username": "TestUser"})
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.Player
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Username != "TestUser" {
		t.Errorf("expected username TestUser, got %s", resp.Username)
	}
}

func TestAuthHandler_Register_MissingUsername(t *testing.T) {
	r, _ := setupAuthRouter()

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAuthHandler_Register_Duplicate(t *testing.T) {
	r, _ := setupAuthRouter()

	body, _ := json.Marshal(map[string]string{"username": "TestUser"})

	// First registration
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("first register: expected 201, got %d", w.Code)
	}

	// Second registration
	body, _ = json.Marshal(map[string]string{"username": "AnotherUser"})
	req = httptest.NewRequest("POST", "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("second register: expected 409, got %d", w.Code)
	}
}

func TestAuthHandler_Login_Success(t *testing.T) {
	r, _ := setupAuthRouter()

	// Register first
	body, _ := json.Marshal(map[string]string{"username": "LoginUser"})
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Login
	req = httptest.NewRequest("POST", "/auth/login", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthHandler_Login_NotRegistered(t *testing.T) {
	r, _ := setupAuthRouter()

	req := httptest.NewRequest("POST", "/auth/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// PlayerHandler tests
// ---------------------------------------------------------------------------

func setupPlayerRouter() *gin.Engine {
	playerRepo := repository.NewMockPlayerRepository()
	now := time.Now()
	_ = playerRepo.Create(nil, &model.Player{
		PlayerID:    "p1",
		FirebaseUID: "uid-p1",
		Username:    "Alice",
		Level:       1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, &model.PlayerDailyBattle{
		PlayerID:         "p1",
		DailyBattleCount: 3,
		LastResetDate:    civil.DateOf(now.UTC().Add(4 * time.Hour)),
	})

	configRepo := repository.NewMockGameConfigRepository()
	configRepo.SetForTest("free_daily_battle_limit", 10)
	playerService := service.NewPlayerService(playerRepo, configRepo)
	handler := NewPlayerHandler(playerService)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.GET("/player", handler.GetPlayer)
	r.PUT("/player/name", handler.UpdateName)
	r.GET("/player/battle-limit", handler.GetBattleLimit)
	return r
}

func TestPlayerHandler_GetPlayer(t *testing.T) {
	r := setupPlayerRouter()

	req := httptest.NewRequest("GET", "/player", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.Player
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Username != "Alice" {
		t.Errorf("expected username Alice, got %s", resp.Username)
	}
}

func TestPlayerHandler_UpdateName(t *testing.T) {
	r := setupPlayerRouter()

	body, _ := json.Marshal(map[string]string{"name": "Bob"})
	req := httptest.NewRequest("PUT", "/player/name", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.Player
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Username != "Bob" {
		t.Errorf("expected username Bob, got %s", resp.Username)
	}
}

func TestPlayerHandler_UpdateName_MissingBody(t *testing.T) {
	r := setupPlayerRouter()

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("PUT", "/player/name", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestPlayerHandler_GetBattleLimit(t *testing.T) {
	r := setupPlayerRouter()

	req := httptest.NewRequest("GET", "/player/battle-limit", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp service.BattleLimitResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.DailyBattleLimit != 10 {
		t.Errorf("expected limit 10, got %d", resp.DailyBattleLimit)
	}
	if resp.DailyBattleCount != 3 {
		t.Errorf("expected count 3, got %d", resp.DailyBattleCount)
	}
	if !resp.CanBattle {
		t.Error("expected CanBattle=true")
	}
}

// ---------------------------------------------------------------------------
// UserSettingsHandler tests
// ---------------------------------------------------------------------------

func setupUserSettingsRouter() *gin.Engine {
	repo := repository.NewMockUserSettingsRepository()
	handler := NewUserSettingsHandler(repo)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.GET("/player/settings", handler.GetSettings)
	r.PUT("/player/settings", handler.UpdateSettings)
	return r
}

func TestUserSettingsHandler_GetSettings_Default(t *testing.T) {
	r := setupUserSettingsRouter()

	req := httptest.NewRequest("GET", "/player/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp model.UserSettings
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Language != "ja" {
		t.Errorf("expected default language ja, got %s", resp.Language)
	}
	if resp.BgmVolume != 50 {
		t.Errorf("expected default BgmVolume 50, got %d", resp.BgmVolume)
	}
}

func TestUserSettingsHandler_UpdateSettings(t *testing.T) {
	r := setupUserSettingsRouter()

	body, _ := json.Marshal(map[string]interface{}{
		"language":     "en",
		"bgm_volume":   80,
		"se_volume":    30,
		"push_enabled": false,
	})
	req := httptest.NewRequest("PUT", "/player/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.UserSettings
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Language != "en" {
		t.Errorf("expected language en, got %s", resp.Language)
	}
	if resp.BgmVolume != 80 {
		t.Errorf("expected BgmVolume 80, got %d", resp.BgmVolume)
	}

	// Verify persistence via GET
	req = httptest.NewRequest("GET", "/player/settings", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Language != "en" {
		t.Errorf("expected persisted language en, got %s", resp.Language)
	}
}

func TestUserSettingsHandler_UpdateSettings_MissingLanguage(t *testing.T) {
	r := setupUserSettingsRouter()

	body, _ := json.Marshal(map[string]interface{}{
		"bgm_volume": 80,
	})
	req := httptest.NewRequest("PUT", "/player/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
