package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// stubBattleClient は service.BattleClient interface の最小スタブ。
// gateway 側に既に interface があるためテスト専用のスタブで十分。
type stubBattleClient struct {
	service.BattleClient // 未使用メソッドは embedded nil が呼ばれると panic するが、テストでは触らない
	npcModels            json.RawMessage
	npcModelsErr         error
	gameLog              json.RawMessage
	gameLogErr           error
	gameLogText          []byte
	gameLogTextErr       error
}

func (s *stubBattleClient) GetNPCModels(ctx context.Context) (json.RawMessage, error) {
	return s.npcModels, s.npcModelsErr
}
func (s *stubBattleClient) GetGameLog(ctx context.Context, gameID string) (json.RawMessage, error) {
	return s.gameLog, s.gameLogErr
}
func (s *stubBattleClient) GetGameLogText(ctx context.Context, gameID string) ([]byte, error) {
	return s.gameLogText, s.gameLogTextErr
}

func TestNPCHandler_GetNPCModels(t *testing.T) {
	t.Run("success returns json passthrough", func(t *testing.T) {
		stub := &stubBattleClient{npcModels: json.RawMessage(`[{"id":"a"}]`)}
		h := NewNPCHandler(stub)
		r := gin.New()
		r.GET("/npc", h.GetNPCModels)

		req := httptest.NewRequest(http.MethodGet, "/npc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, `[{"id":"a"}]`, w.Body.String())
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	})

	t.Run("nil result returns 404", func(t *testing.T) {
		stub := &stubBattleClient{npcModels: nil}
		h := NewNPCHandler(stub)
		r := gin.New()
		r.GET("/npc", h.GetNPCModels)

		req := httptest.NewRequest(http.MethodGet, "/npc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("error returns 500", func(t *testing.T) {
		stub := &stubBattleClient{npcModelsErr: errors.New("boom")}
		h := NewNPCHandler(stub)
		r := gin.New()
		r.GET("/npc", h.GetNPCModels)

		req := httptest.NewRequest(http.MethodGet, "/npc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
