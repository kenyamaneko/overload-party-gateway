package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGameLogHandler_GetGameLog(t *testing.T) {
	t.Run("success returns json passthrough", func(t *testing.T) {
		stub := &stubBattleClient{gameLog: json.RawMessage(`{"events":[]}`)}
		h := NewGameLogHandler(stub)
		r := gin.New()
		r.GET("/games/:gameId/log", h.GetGameLog)

		req := httptest.NewRequest(http.MethodGet, "/games/g1/log", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, `{"events":[]}`, w.Body.String())
	})

	t.Run("nil returns 404", func(t *testing.T) {
		stub := &stubBattleClient{gameLog: nil}
		h := NewGameLogHandler(stub)
		r := gin.New()
		r.GET("/games/:gameId/log", h.GetGameLog)

		req := httptest.NewRequest(http.MethodGet, "/games/g1/log", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("error returns 500", func(t *testing.T) {
		stub := &stubBattleClient{gameLogErr: errors.New("downstream")}
		h := NewGameLogHandler(stub)
		r := gin.New()
		r.GET("/games/:gameId/log", h.GetGameLog)

		req := httptest.NewRequest(http.MethodGet, "/games/g1/log", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGameLogHandler_GetGameLogText(t *testing.T) {
	t.Run("success returns text/plain", func(t *testing.T) {
		stub := &stubBattleClient{gameLogText: []byte("turn 1: ...")}
		h := NewGameLogHandler(stub)
		r := gin.New()
		r.GET("/games/:gameId/log/text", h.GetGameLogText)

		req := httptest.NewRequest(http.MethodGet, "/games/g1/log/text", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "turn 1: ...", w.Body.String())
		assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	})

	t.Run("nil returns 404", func(t *testing.T) {
		stub := &stubBattleClient{gameLogText: nil}
		h := NewGameLogHandler(stub)
		r := gin.New()
		r.GET("/games/:gameId/log/text", h.GetGameLogText)

		req := httptest.NewRequest(http.MethodGet, "/games/g1/log/text", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("error returns 500", func(t *testing.T) {
		stub := &stubBattleClient{gameLogTextErr: errors.New("x")}
		h := NewGameLogHandler(stub)
		r := gin.New()
		r.GET("/games/:gameId/log/text", h.GetGameLogText)

		req := httptest.NewRequest(http.MethodGet, "/games/g1/log/text", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
