package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	"github.com/kenyamaneko/overload-party-card/packages/api-card/apicardserverfake"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
)

// cardFakeRecorder は apicardserverfake に stateful な「最後に受け取った
// Create / Update request」観測を付ける補助。handler テストが「gateway が
// downstream へ正しく body を転送したか」を検証するために使う。
type cardFakeRecorder struct {
	mu             sync.Mutex
	lastCreateBody apicard.DeckCreateRequest
	lastUpdateBody apicard.DeckUpdateRequest
}

func (r *cardFakeRecorder) recordCreate(req apicard.DeckCreateRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastCreateBody = req
}
func (r *cardFakeRecorder) recordUpdate(req apicard.DeckUpdateRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastUpdateBody = req
}
func (r *cardFakeRecorder) create() apicard.DeckCreateRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastCreateBody
}
func (r *cardFakeRecorder) update() apicard.DeckUpdateRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastUpdateBody
}

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
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		fc.ListDecksFn = func() (int, any) {
			return http.StatusOK, []*apicard.Deck{{DeckID: 1, DeckName: "test"}}
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

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
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		fc.ListDecksFn = func() (int, any) {
			return http.StatusInternalServerError, nil
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

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
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		h := NewDeckHandler(cardclient.New(fc.URL()))

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.GET("/decks/:deckId", h.GetDeck)

		req := httptest.NewRequest(http.MethodGet, "/decks/abc", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		fc.GetDeckFn = func(_ string) (int, any) {
			return http.StatusNotFound, nil
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.GET("/decks/:deckId", h.GetDeck)

		req := httptest.NewRequest(http.MethodGet, "/decks/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		fc.GetDeckFn = func(_ string) (int, any) {
			return http.StatusOK, apicardserverfake.DeckWithCardsResponse{
				Deck:  &apicard.Deck{DeckID: 1, DeckName: "d"},
				Cards: []apicard.DeckCard{},
			}
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

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
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		h := NewDeckHandler(cardclient.New(fc.URL()))

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
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		rec := &cardFakeRecorder{}
		fc.CreateDeckFn = func(req apicard.DeckCreateRequest) (int, any) {
			rec.recordCreate(req)
			return http.StatusCreated, apicard.Deck{DeckID: 7, DeckName: "newdeck"}
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

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
		// 下流に正しく転送されたか確認。
		created := rec.create()
		assert.Equal(t, "newdeck", created.DeckName)
		require.Len(t, created.Cards, 1)
		assert.Equal(t, "c1", created.Cards[0].CardID)
		assert.Equal(t, 2, created.Cards[0].Count)
	})

	t.Run("downstream 404 returns 404", func(t *testing.T) {
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		fc.CreateDeckFn = func(_ apicard.DeckCreateRequest) (int, any) {
			return http.StatusNotFound, nil
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

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
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		h := NewDeckHandler(cardclient.New(fc.URL()))
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
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		rec := &cardFakeRecorder{}
		fc.UpdateDeckFn = func(_ string, req apicard.DeckUpdateRequest) (int, any) {
			rec.recordUpdate(req)
			return http.StatusOK, apicard.Deck{DeckID: 5, DeckName: "u"}
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.PUT("/decks/:deckId", h.UpdateDeck)

		req := httptest.NewRequest(http.MethodPut, "/decks/5", strings.NewReader(`{"deck_name":"u","cards":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "u", rec.update().DeckName)
	})
}

func TestDeckHandler_DeleteDeck(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		h := NewDeckHandler(cardclient.New(fc.URL()))

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.DELETE("/decks/:deckId", h.DeleteDeck)

		req := httptest.NewRequest(http.MethodDelete, "/decks/3", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("invalid id", func(t *testing.T) {
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		h := NewDeckHandler(cardclient.New(fc.URL()))

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.DELETE("/decks/:deckId", h.DeleteDeck)

		req := httptest.NewRequest(http.MethodDelete, "/decks/xx", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		fc := apicardserverfake.NewServer()
		defer fc.Close()
		fc.DeleteDeckFn = func(_ string) (int, any) {
			return http.StatusNotFound, nil
		}
		h := NewDeckHandler(cardclient.New(fc.URL()))

		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.DELETE("/decks/:deckId", h.DeleteDeck)

		req := httptest.NewRequest(http.MethodDelete, "/decks/3", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
