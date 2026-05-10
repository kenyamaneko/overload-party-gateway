// Package apigatewayserverfake は gateway サービスの HTTP 契約を実装する
// httptest.Server ラッパー。consumer (client / 連携テスト等) が gateway を
// 呼び出すコードのテストで、実 gateway を起動せずに REST 呼び出しを検証する
// ためのテストダブルを提供する。
//
// 各 endpoint は Fn field (func callback) で status + response body を制御する。
// Fn が nil の endpoint は既定値を返す (happy-path を仮定した最低限の応答)。
//
// Request / Response 型は packages/api-gateway の公開型 (apigateway.PlayerResponse
// 等) を再利用するため、本パッケージは自前の型を宣言していない。
package apigatewayserverfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

// Server は gateway HTTP 契約を実装する httptest.Server wrapper。
type Server struct {
	mu  sync.Mutex
	srv *httptest.Server

	// ─── Public (auth 不要) ──────────────────────────────
	HealthFn            func() (int, any)
	GetVersionFn        func() (int, any)
	ListAnnouncementsFn func() (int, any)
	GetDailyTipFn       func() (int, any)
	ListCloudNewsFn     func() (int, any)

	// ─── Auth (Firebase 認証) ────────────────────────────
	RegisterFn func() (int, any)
	LoginFn    func() (int, any)

	// ─── Authenticated player ────────────────────────────
	GetPlayerFn       func() (int, any)
	UpdateNameFn      func(req apigateway.PlayerNameRequest) (int, any)
	GetBattleLimitFn  func() (int, any)
	ListPlayerCardsFn func() (int, any)
	GetSettingsFn     func() (int, any)
	UpdateSettingsFn  func(req apigateway.UpdateSettingsRequest) (int, any)

	// ─── Decks ───────────────────────────────────────────
	ListDecksFn  func() (int, any)
	GetDeckFn    func(deckID string) (int, any)
	CreateDeckFn func(req apigateway.DeckCreateRequest) (int, any)
	UpdateDeckFn func(deckID string, req apigateway.DeckUpdateRequest) (int, any)
	DeleteDeckFn func(deckID string) (int, any)

	// ─── Cards / NPC / Spectate / Game log (battle proxy) ─
	ListAllCardsFn      func() (int, any)
	GetGameLogFn        func(gameID string) (int, any)
	GetGameLogTextFn    func(gameID string) (int, []byte)
	ListNpcModelsFn     func() (int, any)
	ListSpectateGamesFn func() (int, any)

	// ─── Shop / Faction ───────────────────────────────────
	SelectFactionFn    func(req apigateway.SelectFactionRequest) (int, any)
	ListShopProductsFn func() (int, any)
	PurchaseFn         func(req apigateway.PurchaseRequest) (int, any)
	SubscribeFn        func(req apigateway.PurchaseRequest) (int, any)

	// ─── Scenario ────────────────────────────────────────
	ListScenariosFn     func() (int, any)
	GetScenarioScriptFn func(episodeID string) (int, any)
	CompleteScenarioFn  func(episodeID string) (int, any)

	// ─── Onboarding ──────────────────────────────────────
	GetOnboardingStatusFn func() (int, any)
	GetOnboardingScriptFn func() (int, any)
	GetOnboardingResumeFn func() (int, any)
	SetOnboardingNameFn   func(req apigateway.OnboardingNameRequest) (int, any)
	CompleteOnboardingFn  func(req apigateway.OnboardingCompleteRequest) (int, any)
}

// NewServer は起動済み Server を返す。テスト終了時に Close() すること。
func NewServer() *Server {
	s := &Server{}
	mux := http.NewServeMux()

	// 静的パス。ServeMux のパターン衝突を避けるため、パラメータ付き path は catch-all で dispatch する。
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/announcements", s.handleAnnouncements)
	mux.HandleFunc("GET /api/v1/daily", s.handleDaily)
	mux.HandleFunc("GET /api/v1/cloud-news", s.handleCloudNews)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/v1/player", s.handleGetPlayer)
	mux.HandleFunc("PUT /api/v1/player/name", s.handleUpdateName)
	mux.HandleFunc("GET /api/v1/player/battle-limit", s.handleGetBattleLimit)
	mux.HandleFunc("GET /api/v1/player/cards", s.handleListPlayerCards)
	mux.HandleFunc("GET /api/v1/player/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/v1/player/settings", s.handleUpdateSettings)
	mux.HandleFunc("POST /api/v1/player/select-faction", s.handleSelectFaction)
	mux.HandleFunc("GET /api/v1/player/decks", s.handleListDecks)
	mux.HandleFunc("POST /api/v1/player/decks", s.handleCreateDeck)
	mux.HandleFunc("GET /api/v1/cards", s.handleListAllCards)
	mux.HandleFunc("GET /api/v1/npc/models", s.handleListNpcModels)
	mux.HandleFunc("GET /api/v1/spectate/games", s.handleListSpectateGames)
	mux.HandleFunc("GET /api/v1/shop/products", s.handleListShopProducts)
	mux.HandleFunc("POST /api/v1/shop/purchase", s.handlePurchase)
	mux.HandleFunc("POST /api/v1/shop/subscribe", s.handleSubscribe)
	mux.HandleFunc("GET /api/v1/scenarios", s.handleListScenarios)
	mux.HandleFunc("GET /api/v1/onboarding/status", s.handleOnboardingStatus)
	mux.HandleFunc("GET /api/v1/onboarding/script", s.handleOnboardingScript)
	mux.HandleFunc("GET /api/v1/onboarding/resume", s.handleOnboardingResume)
	mux.HandleFunc("PUT /api/v1/onboarding/name", s.handleOnboardingName)
	mux.HandleFunc("POST /api/v1/onboarding/complete", s.handleOnboardingComplete)

	// パラメータ付き path は catch-all で受け、自前で dispatch する。
	mux.HandleFunc("/api/v1/player/decks/", s.handleDeckByID)
	mux.HandleFunc("/api/v1/games/", s.handleGameByID)
	mux.HandleFunc("/api/v1/scenarios/", s.handleScenarioByID)

	s.srv = httptest.NewServer(mux)
	return s
}

// URL は httptest.Server のベース URL を返す。
func (s *Server) URL() string { return s.srv.URL }

// Close は内部 httptest.Server を閉じる。
func (s *Server) Close() { s.srv.Close() }

// ─── helpers ─────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

func decode[T any](r *http.Request) T {
	var out T
	_ = json.NewDecoder(r.Body).Decode(&out)
	return out
}

// ─── handlers ────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if fn := s.HealthFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.HealthResponse{Status: "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetVersionFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.VersionResponse{})
}

func (s *Server) handleAnnouncements(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListAnnouncementsFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.Announcement{})
}

func (s *Server) handleDaily(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetDailyTipFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.DailyTip{})
}

func (s *Server) handleCloudNews(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListCloudNewsFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.NewsArticle{})
}

func (s *Server) handleRegister(w http.ResponseWriter, _ *http.Request) {
	if fn := s.RegisterFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusCreated, apigateway.PlayerResponse{})
}

func (s *Server) handleLogin(w http.ResponseWriter, _ *http.Request) {
	if fn := s.LoginFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.PlayerResponse{})
}

func (s *Server) handleGetPlayer(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetPlayerFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.PlayerResponse{})
}

func (s *Server) handleUpdateName(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.PlayerNameRequest](r)
	if fn := s.UpdateNameFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.PlayerResponse{})
}

func (s *Server) handleGetBattleLimit(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetBattleLimitFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.BattleLimitResponse{})
}

func (s *Server) handleListPlayerCards(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListPlayerCardsFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.PlayerCardWithDef{})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetSettingsFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.PlayerSettings{})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.UpdateSettingsRequest](r)
	if fn := s.UpdateSettingsFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.PlayerSettings{})
}

func (s *Server) handleSelectFaction(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.SelectFactionRequest](r)
	if fn := s.SelectFactionFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.SelectFactionResponse{})
}

func (s *Server) handleListDecks(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListDecksFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.Deck{})
}

func (s *Server) handleCreateDeck(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.DeckCreateRequest](r)
	if fn := s.CreateDeckFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusCreated, apigateway.Deck{})
}

func (s *Server) handleDeckByID(w http.ResponseWriter, r *http.Request) {
	deckID := strings.TrimPrefix(r.URL.Path, "/api/v1/player/decks/")
	switch r.Method {
	case http.MethodGet:
		if fn := s.GetDeckFn; fn != nil {
			status, body := fn(deckID); writeJSON(w, status, body)
			return
		}
		writeJSON(w, http.StatusOK, apigateway.DeckDetailResponse{})
	case http.MethodPut:
		req := decode[apigateway.DeckUpdateRequest](r)
		if fn := s.UpdateDeckFn; fn != nil {
			status, body := fn(deckID, req); writeJSON(w, status, body)
			return
		}
		writeJSON(w, http.StatusOK, apigateway.Deck{})
	case http.MethodDelete:
		if fn := s.DeleteDeckFn; fn != nil {
			status, body := fn(deckID); writeJSON(w, status, body)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListAllCards(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListAllCardsFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.PlayerCardWithDef{})
}

func (s *Server) handleListNpcModels(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListNpcModelsFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.NpcModel{})
}

func (s *Server) handleListSpectateGames(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListSpectateGamesFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.SpectateGameInfo{})
}

func (s *Server) handleGameByID(w http.ResponseWriter, r *http.Request) {
	// パス: /api/v1/games/{gameId}/log[/text]
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/games/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[1] != "log" {
		http.NotFound(w, r)
		return
	}
	gameID := parts[0]
	if len(parts) == 3 && parts[2] == "text" {
		if fn := s.GetGameLogTextFn; fn != nil {
			status, body := fn(gameID)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(status)
			_, _ = w.Write(body)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		return
	}
	if fn := s.GetGameLogFn; fn != nil {
		status, body := fn(gameID); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleListShopProducts(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListShopProductsFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.ProductListResponse{})
}

func (s *Server) handlePurchase(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.PurchaseRequest](r)
	if fn := s.PurchaseFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.PurchaseResponse{})
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.PurchaseRequest](r)
	if fn := s.SubscribeFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.SubscribeResponse{})
}

func (s *Server) handleListScenarios(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListScenariosFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.EpisodeListResponse{})
}

func (s *Server) handleScenarioByID(w http.ResponseWriter, r *http.Request) {
	// パス: /api/v1/scenarios/{episodeId}/{action}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/scenarios/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	episodeID := parts[0]
	switch parts[1] {
	case "script":
		if fn := s.GetScenarioScriptFn; fn != nil {
			status, body := fn(episodeID); writeJSON(w, status, body)
			return
		}
		writeJSON(w, http.StatusOK, apigateway.ScenarioScriptResponse{})
	case "complete":
		if fn := s.CompleteScenarioFn; fn != nil {
			status, body := fn(episodeID); writeJSON(w, status, body)
			return
		}
		writeJSON(w, http.StatusOK, apigateway.ScenarioCompleteResponse{})
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleOnboardingStatus(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetOnboardingStatusFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.OnboardingStatus{})
}

func (s *Server) handleOnboardingScript(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetOnboardingScriptFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.OnboardingScriptResponse{})
}

func (s *Server) handleOnboardingResume(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetOnboardingResumeFn; fn != nil {
		status, body := fn(); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.OnboardingResumeResponse{})
}

func (s *Server) handleOnboardingName(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.OnboardingNameRequest](r)
	if fn := s.SetOnboardingNameFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	req := decode[apigateway.OnboardingCompleteRequest](r)
	if fn := s.CompleteOnboardingFn; fn != nil {
		status, body := fn(req); writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.OnboardingCompleteResponse{})
}
