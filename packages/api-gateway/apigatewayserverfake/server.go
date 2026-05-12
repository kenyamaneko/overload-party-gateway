// Package apigatewayserverfake は gateway サービスの HTTP 契約を実装する
// httptest.Server ラッパー。consumer (client / 連携テスト等) が gateway を
// 呼び出すコードのテストで、実 gateway を起動せずに REST 呼び出しを検証する
// ためのテストダブルを提供する。
//
// 各 endpoint は Fn field (func callback) で status + response body を制御する。
// Fn が nil の endpoint は既定値を返す (happy-path を仮定した最低限の応答)。
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
	HealthFn     func() (int, any)
	GetVersionFn func() (int, any)

	// ─── Auth (Firebase 認証) ────────────────────────────
	RegisterFn func() (int, any)
	LoginFn    func() (int, any)

	// ─── NPC / Spectate / Game log (battle proxy) ─
	GetGameLogFn        func(gameID string) (int, any)
	GetGameLogTextFn    func(gameID string) (int, []byte)
	ListNpcModelsFn     func() (int, any)
	ListSpectateGamesFn func() (int, any)
}

// NewServer は起動済み Server を返す。テスト終了時に Close() すること。
func NewServer() *Server {
	s := &Server{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("GET /api/v1/npc/models", s.handleListNpcModels)
	mux.HandleFunc("GET /api/v1/spectate/games", s.handleListSpectateGames)

	// パラメータ付き path は catch-all で受け、自前で dispatch する。
	mux.HandleFunc("/api/v1/games/", s.handleGameByID)

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

// ─── handlers ────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if fn := s.HealthFn; fn != nil {
		status, body := fn()
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.HealthResponse{Status: "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	if fn := s.GetVersionFn; fn != nil {
		status, body := fn()
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.VersionResponse{})
}

func (s *Server) handleRegister(w http.ResponseWriter, _ *http.Request) {
	if fn := s.RegisterFn; fn != nil {
		status, body := fn()
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusCreated, apigateway.PlayerResponse{})
}

func (s *Server) handleLogin(w http.ResponseWriter, _ *http.Request) {
	if fn := s.LoginFn; fn != nil {
		status, body := fn()
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, apigateway.PlayerResponse{})
}

func (s *Server) handleListNpcModels(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListNpcModelsFn; fn != nil {
		status, body := fn()
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, []apigateway.NpcModel{})
}

func (s *Server) handleListSpectateGames(w http.ResponseWriter, _ *http.Request) {
	if fn := s.ListSpectateGamesFn; fn != nil {
		status, body := fn()
		writeJSON(w, status, body)
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
		status, body := fn(gameID)
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
