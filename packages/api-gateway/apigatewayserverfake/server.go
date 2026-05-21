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
}

// NewServer は起動済み Server を返す。テスト終了時に Close() すること。
func NewServer() *Server {
	s := &Server{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("POST /api/v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

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
