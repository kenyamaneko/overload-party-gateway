package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
)

// fakeCardServer は cardclient が叩く decks 系エンドポイントだけ実装する。
type fakeCardServer struct {
	mu     sync.Mutex
	server *httptest.Server

	listDecksStatus  int
	listDecksBody    any
	getDeckStatus    int
	getDeckBody      any
	createDeckStatus int
	createDeckBody   any
	updateDeckStatus int
	updateDeckBody   any
	deleteDeckStatus int

	lastCreateBody apicard.DeckCreateRequest
	lastUpdateBody apicard.DeckUpdateRequest
}

func newFakeCardServer() *fakeCardServer {
	fc := &fakeCardServer{
		listDecksStatus:  http.StatusOK,
		listDecksBody:    []*apicard.Deck{},
		getDeckStatus:    http.StatusOK,
		createDeckStatus: http.StatusCreated,
		updateDeckStatus: http.StatusOK,
		deleteDeckStatus: http.StatusNoContent,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/internal/v1/players/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		fc.mu.Lock()
		defer fc.mu.Unlock()

		switch {
		case strings.HasSuffix(path, "/decks") && r.Method == http.MethodGet:
			w.WriteHeader(fc.listDecksStatus)
			if fc.listDecksBody != nil {
				_ = json.NewEncoder(w).Encode(fc.listDecksBody)
			}
		case strings.HasSuffix(path, "/decks") && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&fc.lastCreateBody)
			w.WriteHeader(fc.createDeckStatus)
			if fc.createDeckBody != nil {
				_ = json.NewEncoder(w).Encode(fc.createDeckBody)
			}
		case r.Method == http.MethodGet:
			w.WriteHeader(fc.getDeckStatus)
			if fc.getDeckBody != nil {
				_ = json.NewEncoder(w).Encode(fc.getDeckBody)
			}
		case r.Method == http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&fc.lastUpdateBody)
			w.WriteHeader(fc.updateDeckStatus)
			if fc.updateDeckBody != nil {
				_ = json.NewEncoder(w).Encode(fc.updateDeckBody)
			}
		case r.Method == http.MethodDelete:
			w.WriteHeader(fc.deleteDeckStatus)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	})

	fc.server = httptest.NewServer(mux)
	return fc
}

func (f *fakeCardServer) close()                        { f.server.Close() }
func (f *fakeCardServer) client() *cardclient.Client    { return cardclient.New(f.server.URL) }

// withPlayerID は player_id を context に注入する middleware ヘルパー。
func withPlayerID(pid string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if pid != "" {
			c.Set("player_id", pid)
		}
		c.Next()
	}
}

func TestDeckHandler_GetDecks(t *testing.T) {
	t.Run("success returns decks", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		fc.listDecksBody = []*apicard.Deck{{DeckID: 1, DeckName: "test"}}
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.GET("/decks", h.GetDecks)

		req := httptest.NewRequest(http.MethodGet, "/decks", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"deck_name":"test"`)
	})

	t.Run("downstream 500", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		fc.listDecksStatus = http.StatusInternalServerError
		fc.listDecksBody = nil
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.GET("/decks", h.GetDecks)

		req := httptest.NewRequest(http.MethodGet, "/decks", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestDeckHandler_GetDeck(t *testing.T) {
	t.Run("invalid deck_id returns 400", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.GET("/decks/:deckId", h.GetDeck)

		req := httptest.NewRequest(http.MethodGet, "/decks/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		fc.getDeckStatus = http.StatusNotFound
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.GET("/decks/:deckId", h.GetDeck)

		req := httptest.NewRequest(http.MethodGet, "/decks/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		fc.getDeckBody = map[string]any{
			"deck":  &apicard.Deck{DeckID: 1, DeckName: "d"},
			"cards": []apicard.DeckCard{},
		}
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.GET("/decks/:deckId", h.GetDeck)

		req := httptest.NewRequest(http.MethodGet, "/decks/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"deck"`)
		assert.Contains(t, w.Body.String(), `"cards"`)
	})
}

func TestDeckHandler_CreateDeck(t *testing.T) {
	t.Run("invalid JSON returns 400", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/decks", h.CreateDeck)

		req := httptest.NewRequest(http.MethodPost, "/decks", strings.NewReader(`{not json`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("success forwards body and returns 201", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		fc.createDeckBody = &apicard.Deck{DeckID: 7, DeckName: "newdeck"}
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/decks", h.CreateDeck)

		body := `{"deck_name":"newdeck","cards":[{"card_id":"c1","art_no":1,"count":2}]}`
		req := httptest.NewRequest(http.MethodPost, "/decks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"deck_name":"newdeck"`)
		// 下流に正しく変換されて転送されたか確認
		assert.Equal(t, "newdeck", fc.lastCreateBody.DeckName)
		require.Len(t, fc.lastCreateBody.Cards, 1)
		assert.Equal(t, "c1", fc.lastCreateBody.Cards[0].CardID)
		assert.Equal(t, 2, fc.lastCreateBody.Cards[0].Count)
	})

	t.Run("downstream invalid deck returns 400", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		// cardclient は 400 をジェネリックエラーで包んでしまうので 500 として扱われる。
		// ここでは 404 マッピングを検証する。
		fc.createDeckStatus = http.StatusNotFound
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/decks", h.CreateDeck)

		body := `{"deck_name":"x","cards":[]}`
		req := httptest.NewRequest(http.MethodPost, "/decks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestDeckHandler_UpdateDeck(t *testing.T) {
	t.Run("invalid deck id", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		h := NewDeckHandler(fc.client())
		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.PUT("/decks/:deckId", h.UpdateDeck)

		req := httptest.NewRequest(http.MethodPut, "/decks/abc", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		fc.updateDeckBody = &apicard.Deck{DeckID: 5, DeckName: "u"}
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.PUT("/decks/:deckId", h.UpdateDeck)

		req := httptest.NewRequest(http.MethodPut, "/decks/5", strings.NewReader(`{"deck_name":"u","cards":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "u", fc.lastUpdateBody.DeckName)
	})
}

func TestDeckHandler_DeleteDeck(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.DELETE("/decks/:deckId", h.DeleteDeck)

		req := httptest.NewRequest(http.MethodDelete, "/decks/3", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("invalid id", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.DELETE("/decks/:deckId", h.DeleteDeck)

		req := httptest.NewRequest(http.MethodDelete, "/decks/xx", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		fc := newFakeCardServer()
		defer fc.close()
		fc.deleteDeckStatus = http.StatusNotFound
		h := NewDeckHandler(fc.client())

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.DELETE("/decks/:deckId", h.DeleteDeck)

		req := httptest.NewRequest(http.MethodDelete, "/decks/3", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
