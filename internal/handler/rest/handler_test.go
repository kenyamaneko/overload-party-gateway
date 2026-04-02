package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
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
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})
	handler := NewAuthHandler(authService)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.POST("/auth/register", handler.Register)
	r.POST("/auth/login", handler.Login)
	return r, playerRepo
}

func TestAuthHandler_Register(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]string
		wantCode   int
		wantName   string // empty means skip body check
	}{
		{"success", map[string]string{"username": "TestUser"}, http.StatusCreated, "TestUser"},
		{"missing username", map[string]string{}, http.StatusBadRequest, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := setupAuthRouter()

			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			require.Equal(t, tt.wantCode, w.Code, w.Body.String())
			if tt.wantName != "" {
				var resp model.Player
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.wantName, resp.Username)
			}
		})
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

	require.Equal(t, http.StatusCreated, w.Code)

	// Second registration
	body, _ = json.Marshal(map[string]string{"username": "AnotherUser"})
	req = httptest.NewRequest("POST", "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
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

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAuthHandler_Login_NotRegistered(t *testing.T) {
	r, _ := setupAuthRouter()

	req := httptest.NewRequest("POST", "/auth/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ---------------------------------------------------------------------------
// PlayerHandler tests
// ---------------------------------------------------------------------------

func setupPlayerRouter() *gin.Engine {
	playerRepo := repository.NewMockPlayerRepository()
	now := time.Now()
	_ = playerRepo.Create(context.TODO(), &model.Player{
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
	configRepo.SetForTest("exp_formula_coefficient", 60)
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

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp model.Player
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Alice", resp.Username)
}

func TestPlayerHandler_UpdateName(t *testing.T) {
	r := setupPlayerRouter()

	body, _ := json.Marshal(map[string]string{"name": "Bob"})
	req := httptest.NewRequest("PUT", "/player/name", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp model.Player
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Bob", resp.Username)
}

func TestPlayerHandler_UpdateName_MissingBody(t *testing.T) {
	r := setupPlayerRouter()

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("PUT", "/player/name", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPlayerHandler_GetBattleLimit(t *testing.T) {
	r := setupPlayerRouter()

	req := httptest.NewRequest("GET", "/player/battle-limit", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp service.BattleLimitResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, service.BattleLimitResponse{
		DailyBattleLimit: 10,
		DailyBattleCount: 3,
		CanBattle:        true,
	}, resp)
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

	require.Equal(t, http.StatusOK, w.Code)

	var resp model.UserSettings
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "ja", resp.Language)
	assert.Equal(t, int64(50), resp.BgmVolume)
}

func TestUserSettingsHandler_UpdateSettings(t *testing.T) {
	r := setupUserSettingsRouter()

	// When: update settings
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

	// Then: response contains updated values
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp model.UserSettings
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "en", resp.Language)
	assert.Equal(t, int64(80), resp.BgmVolume)
	assert.Equal(t, int64(30), resp.SeVolume)
	assert.False(t, resp.PushEnabled)
}

func TestUserSettingsHandler_UpdateSettings_Persisted(t *testing.T) {
	r := setupUserSettingsRouter()

	// Given: settings have been updated
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
	require.Equal(t, http.StatusOK, w.Code)

	// When: GET the settings
	req = httptest.NewRequest("GET", "/player/settings", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Then: persisted values match
	require.Equal(t, http.StatusOK, w.Code)
	var resp model.UserSettings
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "en", resp.Language)
	assert.Equal(t, int64(80), resp.BgmVolume)
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

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// DeckHandler tests
// ---------------------------------------------------------------------------

func setupDeckRouter() *gin.Engine {
	deckRepo := repository.NewMockDeckRepository()
	playerCardRepo := repository.NewMockPlayerCardRepository()
	cardCache := cache.NewCardCache()

	// Seed 10 card definitions and player cards (3 copies each = 30 total).
	playerCards := make([]*model.PlayerCard, 10)
	for i := 1; i <= 10; i++ {
		cardID := fmt.Sprintf("C-%03d", i)
		cardCache.InjectForTest(cardID, &model.CardDefinition{
			CardID:      cardID,
			CardName:    fmt.Sprintf("Card%d", i),
			Restriction: "none",
		})
		playerCards[i-1] = &model.PlayerCard{CardID: cardID, ArtNo: 1, Count: 3}
	}
	playerCardRepo.SeedPlayerCards("p1", playerCards)

	deckService := service.NewDeckService(deckRepo, playerCardRepo, cardCache)
	handler := NewDeckHandler(deckService)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.GET("/decks", handler.GetDecks)
	r.GET("/decks/:deckId", handler.GetDeck)
	r.POST("/decks", handler.CreateDeck)
	r.PUT("/decks/:deckId", handler.UpdateDeck)
	r.DELETE("/decks/:deckId", handler.DeleteDeck)
	return r
}

func TestDeckHandler_GetDecks_Empty(t *testing.T) {
	r := setupDeckRouter()

	req := httptest.NewRequest("GET", "/decks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp []*model.Deck
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp)
}

func TestDeckHandler_CreateDeck_Success(t *testing.T) {
	r := setupDeckRouter()

	entries := make([]service.DeckCardEntry, 10)
	for i := 1; i <= 10; i++ {
		entries[i-1] = service.DeckCardEntry{CardID: fmt.Sprintf("C-%03d", i), ArtNo: 1, Count: 3}
	}
	body, _ := json.Marshal(service.CreateDeckRequest{
		DeckName: "MyDeck",
		Cards:    entries,
	})
	req := httptest.NewRequest("POST", "/decks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var resp model.Deck
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "MyDeck", resp.DeckName)
	assert.True(t, resp.DeckID > 0)
}

func TestDeckHandler_CreateDeck_MissingBody(t *testing.T) {
	r := setupDeckRouter()

	req := httptest.NewRequest("POST", "/decks", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeckHandler_InvalidDeckID(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{"GET", "GET"},
		{"DELETE", "DELETE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupDeckRouter()
			req := httptest.NewRequest(tt.method, "/decks/abc", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// NewsHandler tests
// ---------------------------------------------------------------------------

type mockNewsRepo struct {
	articles []*model.NewsArticle
}

func (m *mockNewsRepo) List(_ context.Context, limit, offset int) ([]*model.NewsArticle, error) {
	if offset >= len(m.articles) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.articles) {
		end = len(m.articles)
	}
	return m.articles[offset:end], nil
}

func TestNewsHandler_GetCloudNews_Empty(t *testing.T) {
	repo := &mockNewsRepo{}
	handler := NewNewsHandler(repo)

	r := gin.New()
	r.GET("/news", handler.GetCloudNews)

	req := httptest.NewRequest("GET", "/news", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `[]`, w.Body.String())
}

func TestNewsHandler_GetCloudNews_WithArticles(t *testing.T) {
	now := time.Now()
	repo := &mockNewsRepo{
		articles: []*model.NewsArticle{
			{
				ArticleID: "a1",
				Source:    "test",
				Title:     "First Article",
				Tags:      []string{"update"},
				FetchedAt: now,
			},
			{
				ArticleID: "a2",
				Source:    "test",
				Title:     "Second Article",
				Tags:      []string{"event"},
				FetchedAt: now,
			},
		},
	}
	handler := NewNewsHandler(repo)

	r := gin.New()
	r.GET("/news", handler.GetCloudNews)

	req := httptest.NewRequest("GET", "/news", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp []model.NewsArticle
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp, 2)
	assert.Equal(t, "First Article", resp[0].Title)
	assert.Equal(t, "Second Article", resp[1].Title)
}

// ---------------------------------------------------------------------------
// GameLogHandler tests
// ---------------------------------------------------------------------------

type mockBattleClient struct {
	service.BattleClient // embed to satisfy interface
	gameLogs     map[string]json.RawMessage
	gameLogTexts map[string][]byte
}

func (m *mockBattleClient) GetGameLog(_ context.Context, gameID string) (json.RawMessage, error) {
	if v, ok := m.gameLogs[gameID]; ok {
		return v, nil
	}
	return nil, nil
}

func (m *mockBattleClient) GetGameLogText(_ context.Context, gameID string) ([]byte, error) {
	if v, ok := m.gameLogTexts[gameID]; ok {
		return v, nil
	}
	return nil, nil
}

func setupGameLogRouter(bc service.BattleClient) *gin.Engine {
	handler := NewGameLogHandler(bc)

	r := gin.New()
	r.GET("/games/:gameId/log", handler.GetGameLog)
	r.GET("/games/:gameId/log/text", handler.GetGameLogText)
	return r
}

func TestGameLogHandler_GetGameLog_NotFound(t *testing.T) {
	bc := &mockBattleClient{
		gameLogs:     map[string]json.RawMessage{},
		gameLogTexts: map[string][]byte{},
	}
	r := setupGameLogRouter(bc)

	req := httptest.NewRequest("GET", "/games/nonexistent/log", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGameLogHandler_GetGameLog_Success(t *testing.T) {
	logData := json.RawMessage(`{"turns":[{"action":"draw"}]}`)
	bc := &mockBattleClient{
		gameLogs:     map[string]json.RawMessage{"game-123": logData},
		gameLogTexts: map[string][]byte{},
	}
	r := setupGameLogRouter(bc)

	req := httptest.NewRequest("GET", "/games/game-123/log", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"turns":[{"action":"draw"}]}`, w.Body.String())
}

func TestGameLogHandler_GetGameLogText_NotFound(t *testing.T) {
	bc := &mockBattleClient{
		gameLogs:     map[string]json.RawMessage{},
		gameLogTexts: map[string][]byte{},
	}
	r := setupGameLogRouter(bc)

	req := httptest.NewRequest("GET", "/games/nonexistent/log/text", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ---------------------------------------------------------------------------
// ShopHandler tests
// ---------------------------------------------------------------------------

type mockReceiptVerifier struct{}

func (m *mockReceiptVerifier) VerifyPurchase(_ context.Context, _ string) (*platform.VerifyResult, error) {
	return &platform.VerifyResult{IsValid: true}, nil
}

func (m *mockReceiptVerifier) VerifySubscription(_ context.Context, _ string) (*platform.SubscriptionInfo, error) {
	return &platform.SubscriptionInfo{IsValid: true, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}, nil
}

func setupShopRouter() *gin.Engine {
	shopRepo := repository.NewMockShopRepository()
	subRepo := repository.NewMockSubscriptionRepository()
	playerRepo := repository.NewMockPlayerRepository()
	factionRepo := repository.NewMockFactionRepository()
	cardCache := cache.NewCardCache()
	verifier := &mockReceiptVerifier{}
	shopService := service.NewShopService(shopRepo, subRepo, playerRepo, factionRepo, &repository.MockTxRunner{}, cardCache, verifier, verifier)
	handler := NewShopHandler(shopService)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.PUT("/shop/faction", handler.SelectFaction)
	r.GET("/shop/products", handler.GetProducts)
	r.POST("/shop/purchase", handler.Purchase)
	r.POST("/shop/subscribe", handler.Subscribe)
	return r
}

func TestShopHandler_SelectFaction_MissingBody(t *testing.T) {
	r := setupShopRouter()

	req := httptest.NewRequest("PUT", "/shop/faction", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestShopHandler_GetProducts_Success(t *testing.T) {
	r := setupShopRouter()

	req := httptest.NewRequest("GET", "/shop/products", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "products")
}

func TestShopHandler_Purchase_MissingBody(t *testing.T) {
	r := setupShopRouter()

	req := httptest.NewRequest("POST", "/shop/purchase", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestShopHandler_Subscribe_MissingBody(t *testing.T) {
	r := setupShopRouter()

	req := httptest.NewRequest("POST", "/shop/subscribe", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// WebhookHandler tests
// ---------------------------------------------------------------------------

func setupWebhookRouter() *gin.Engine {
	subRepo := repository.NewMockSubscriptionRepository()
	playerRepo := repository.NewMockPlayerRepository()
	subService := service.NewSubscriptionService(subRepo, playerRepo, &repository.MockTxRunner{})
	handler := NewWebhookHandler(subService)

	r := gin.New()
	r.POST("/webhooks/apple", handler.HandleAppleWebhook)
	r.POST("/webhooks/google", handler.HandleGoogleWebhook)
	return r
}

func TestWebhookHandler_HandleAppleWebhook_MissingBody(t *testing.T) {
	r := setupWebhookRouter()

	req := httptest.NewRequest("POST", "/webhooks/apple", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWebhookHandler_HandleGoogleWebhook_MissingBody(t *testing.T) {
	r := setupWebhookRouter()

	req := httptest.NewRequest("POST", "/webhooks/google", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// CardHandler tests
// ---------------------------------------------------------------------------

func setupCardRouter() *gin.Engine {
	cardRepo := repository.NewMockCardRepository(map[string]*model.CardDefinition{})
	cardService := service.NewCardService(cardRepo, repository.NewMockPlayerCardRepository())
	handler := NewCardHandler(cardService)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.GET("/cards", handler.GetAllCards)
	return r
}

func TestCardHandler_GetAllCards_Empty(t *testing.T) {
	r := setupCardRouter()

	req := httptest.NewRequest("GET", "/cards", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp []*service.CardWithOwnership
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp)
}

// ---------------------------------------------------------------------------
// PlayerCardHandler tests
// ---------------------------------------------------------------------------

func setupPlayerCardRouter() *gin.Engine {
	deckRepo := repository.NewMockDeckRepository()
	cardCache := cache.NewCardCache()
	deckService := service.NewDeckService(deckRepo, repository.NewMockPlayerCardRepository(), cardCache)
	handler := NewPlayerCardHandler(deckService)

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.GET("/player/cards", handler.GetPlayerCards)
	return r
}

func TestPlayerCardHandler_GetPlayerCards(t *testing.T) {
	r := setupPlayerCardRouter()

	req := httptest.NewRequest("GET", "/player/cards", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// ---------------------------------------------------------------------------
// StoryHandler tests
// ---------------------------------------------------------------------------

func setupStoryRouter() (*gin.Engine, *testStoryEnv) {
	storyRepo := repository.NewMockStoryRepository()
	factionRepo := repository.NewMockFactionRepository()
	playerRepo := repository.NewMockPlayerRepository()
	storyRepo.SetDeps(playerRepo, factionRepo)

	storyService := service.NewStoryService(storyRepo, nil, "")
	handler := NewStoryHandler(storyService)

	now := time.Now()
	_ = playerRepo.Create(context.TODO(), &model.Player{
		PlayerID:    "p1",
		FirebaseUID: "uid-p1",
		Username:    "Alice",
		Level:       5,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, &model.PlayerDailyBattle{PlayerID: "p1"})

	_ = factionRepo.AddPlayerFaction(context.TODO(), "p1", "SHE", "initial_selection")

	faction := "SHE"
	storyRepo.SeedEpisodes([]*model.ScenarioEpisode{
		{
			EpisodeID:        "she_ep1",
			Faction:          &faction,
			EpisodeNumber:    1,
			TitleJa:          "SHE 第1章",
			TitleEn:          "SHE Chapter 1",
			RequiredLevel:    2,
			RequiredFactions: []string{"SHE"},
			RequiredEpisodes: []string{},
			ScriptPath:       "stories/{lang}/she_ep1.ks",
			SortOrder:        1,
			IsActive:         true,
		},
	})

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.GET("/scenarios", handler.ListEpisodes)
	r.GET("/scenarios/:episodeId/script", handler.GetScript)
	r.POST("/scenarios/:episodeId/complete", handler.CompleteEpisode)

	return r, &testStoryEnv{storyRepo: storyRepo, factionRepo: factionRepo, playerRepo: playerRepo}
}

type testStoryEnv struct {
	storyRepo   *repository.MockStoryRepository
	factionRepo *repository.MockFactionRepository
	playerRepo  *repository.MockPlayerRepository
}

func TestStoryHandler_ListEpisodes_Success(t *testing.T) {
	r, _ := setupStoryRouter()

	req := httptest.NewRequest("GET", "/scenarios", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "episodes")

	episodes := resp["episodes"].([]interface{})
	assert.Len(t, episodes, 1)

	ep := episodes[0].(map[string]interface{})
	assert.Equal(t, "she_ep1", ep["episode_id"])
	assert.Equal(t, true, ep["is_unlocked"])
}

func TestStoryHandler_ListEpisodes_WithLang(t *testing.T) {
	r, _ := setupStoryRouter()

	req := httptest.NewRequest("GET", "/scenarios?lang=en", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	episodes := resp["episodes"].([]interface{})
	ep := episodes[0].(map[string]interface{})
	assert.Equal(t, "SHE Chapter 1", ep["title"])
}

func TestStoryHandler_GetScript_NotFound(t *testing.T) {
	r, _ := setupStoryRouter()

	req := httptest.NewRequest("GET", "/scenarios/nonexistent/script", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStoryHandler_CompleteEpisode_Success(t *testing.T) {
	r, _ := setupStoryRouter()

	req := httptest.NewRequest("POST", "/scenarios/she_ep1/complete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "episode completed", resp["message"])
	assert.Equal(t, "she_ep1", resp["episode_id"])
}

func TestStoryHandler_CompleteEpisode_NotFound(t *testing.T) {
	r, _ := setupStoryRouter()

	req := httptest.NewRequest("POST", "/scenarios/nonexistent/complete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStoryHandler_CompleteEpisode_Locked(t *testing.T) {
	storyRepo := repository.NewMockStoryRepository()
	factionRepo := repository.NewMockFactionRepository()
	playerRepo := repository.NewMockPlayerRepository()
	storyRepo.SetDeps(playerRepo, factionRepo)

	storyService := service.NewStoryService(storyRepo, nil, "")
	handler := NewStoryHandler(storyService)

	now := time.Now()
	_ = playerRepo.Create(context.TODO(), &model.Player{
		PlayerID:    "p1",
		FirebaseUID: "uid-p1",
		Level:       1, // too low
		CreatedAt:   now,
		UpdatedAt:   now,
	}, &model.PlayerDailyBattle{PlayerID: "p1"})

	faction := "SHE"
	storyRepo.SeedEpisodes([]*model.ScenarioEpisode{
		{EpisodeID: "she_ep1", Faction: &faction, EpisodeNumber: 1, TitleJa: "SHE 1", TitleEn: "SHE 1", RequiredLevel: 5, RequiredFactions: []string{}, RequiredEpisodes: []string{}, ScriptPath: "s/{lang}/s.ks", SortOrder: 1, IsActive: true},
	})

	r := gin.New()
	r.Use(setPlayerID("p1"))
	r.POST("/scenarios/:episodeId/complete", handler.CompleteEpisode)

	req := httptest.NewRequest("POST", "/scenarios/she_ep1/complete", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
