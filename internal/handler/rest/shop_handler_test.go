package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"
	"github.com/kenyamaneko/overload-party-shop/packages/api-shop/apishopserverfake"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/shopclient"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

func TestShopHandler_SelectFaction(t *testing.T) {
	t.Run("invalid JSON returns 400", func(t *testing.T) {
		fs := apishopserverfake.NewServer()
		defer fs.Close()
		h := NewShopHandler(shopclient.New(fs.URL()))
		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/select-faction", h.SelectFaction)

		req := httptest.NewRequest(http.MethodPost, "/select-faction", strings.NewReader(`not json`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty faction returns 400", func(t *testing.T) {
		fs := apishopserverfake.NewServer()
		defer fs.Close()
		h := NewShopHandler(shopclient.New(fs.URL()))
		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/select-faction", h.SelectFaction)

		req := httptest.NewRequest(http.MethodPost, "/select-faction", strings.NewReader(`{"faction":""}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("downstream conflict (faction already selected) returns 409", func(t *testing.T) {
		fs := apishopserverfake.NewServer()
		defer fs.Close()
		fs.SelectFactionFn = func(_ string, _ apishopserverfake.SelectFactionRequest) (int, any) {
			return http.StatusConflict, map[string]string{"error": "faction already selected"}
		}
		h := NewShopHandler(shopclient.New(fs.URL()))
		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/select-faction", h.SelectFaction)

		req := httptest.NewRequest(http.MethodPost, "/select-faction", strings.NewReader(`{"faction":"f1"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("downstream invalid faction returns 400", func(t *testing.T) {
		fs := apishopserverfake.NewServer()
		defer fs.Close()
		fs.SelectFactionFn = func(_ string, _ apishopserverfake.SelectFactionRequest) (int, any) {
			return http.StatusBadRequest, map[string]string{"error": "invalid faction"}
		}
		h := NewShopHandler(shopclient.New(fs.URL()))
		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/select-faction", h.SelectFaction)

		req := httptest.NewRequest(http.MethodPost, "/select-faction", strings.NewReader(`{"faction":"bogus"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("success returns 200 with shop response", func(t *testing.T) {
		fs := apishopserverfake.NewServer()
		defer fs.Close()
		fs.SelectFactionFn = func(_ string, req apishopserverfake.SelectFactionRequest) (int, any) {
			return http.StatusOK, apishopserverfake.SelectFactionResponse{
				Message: "ok", Faction: req.Faction, CardsGranted: 5,
			}
		}
		h := NewShopHandler(shopclient.New(fs.URL()))
		r := gin.New()
		r.Use(withPlayerID("p1"))
		r.POST("/select-faction", h.SelectFaction)

		req := httptest.NewRequest(http.MethodPost, "/select-faction", strings.NewReader(`{"faction":"red"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"cards_granted":5`)
	})
}

func TestShopHandler_Purchase_Validation(t *testing.T) {
	tests := []struct {
		name string
		req  apigateway.PurchaseRequest
	}{
		{"empty product_id", apigateway.PurchaseRequest{ProductID: "", Platform: "ios", PurchaseToken: "t"}},
		{"empty platform", apigateway.PurchaseRequest{ProductID: "p", Platform: "", PurchaseToken: "t"}},
		{"empty token", apigateway.PurchaseRequest{ProductID: "p", Platform: "ios", PurchaseToken: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := apishopserverfake.NewServer()
			defer fs.Close()
			h := NewShopHandler(shopclient.New(fs.URL()))
			r := gin.New()
			r.Use(withPlayerID("p1"))
			r.POST("/purchase", h.Purchase)

			body, _ := json.Marshal(tt.req)
			req := httptest.NewRequest(http.MethodPost, "/purchase", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

func TestShopHandler_Purchase_Success(t *testing.T) {
	fs := apishopserverfake.NewServer()
	defer fs.Close()
	h := NewShopHandler(shopclient.New(fs.URL()))
	r := gin.New()
	r.Use(withPlayerID("p1"))
	r.POST("/purchase", h.Purchase)

	body := `{"product_id":"p1","platform":"ios","purchase_token":"tok"}`
	req := httptest.NewRequest(http.MethodPost, "/purchase", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"product_id":"p1"`)
}

func TestShopHandler_Purchase_ReceiptInvalid(t *testing.T) {
	fs := apishopserverfake.NewServer()
	defer fs.Close()
	fs.PurchaseFn = func(_ string, _ apishop.PurchaseRequest) (int, any) {
		return http.StatusPaymentRequired, map[string]string{"error": "invalid receipt"}
	}
	h := NewShopHandler(shopclient.New(fs.URL()))
	r := gin.New()
	r.Use(withPlayerID("p1"))
	r.POST("/purchase", h.Purchase)

	body := `{"product_id":"p1","platform":"ios","purchase_token":"tok"}`
	req := httptest.NewRequest(http.MethodPost, "/purchase", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
}

func TestShopHandler_Subscribe_Success(t *testing.T) {
	fs := apishopserverfake.NewServer()
	defer fs.Close()
	exp := time.Now().Add(24 * time.Hour).UTC()
	fs.SubscribeFn = func(_ string, _ apishop.PurchaseRequest) (int, any) {
		return http.StatusOK, apishopserverfake.SubscribeResponse{Message: "ok", ExpiresAt: &exp}
	}
	h := NewShopHandler(shopclient.New(fs.URL()))
	r := gin.New()
	r.Use(withPlayerID("p1"))
	r.POST("/subscribe", h.Subscribe)

	body := `{"product_id":"sub1","platform":"ios","purchase_token":"tok"}`
	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"expires_at"`)
}

func TestShopHandler_Subscribe_Validation(t *testing.T) {
	fs := apishopserverfake.NewServer()
	defer fs.Close()
	h := NewShopHandler(shopclient.New(fs.URL()))
	r := gin.New()
	r.Use(withPlayerID("p1"))
	r.POST("/subscribe", h.Subscribe)

	req := httptest.NewRequest(http.MethodPost, "/subscribe", strings.NewReader(`{"product_id":"","platform":"ios","purchase_token":"t"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestShopHandler_GetProducts(t *testing.T) {
	fs := apishopserverfake.NewServer()
	defer fs.Close()
	fs.GetProductsFn = func(_ string) (int, any) {
		return http.StatusOK, apishopserverfake.ProductsResponse{
			Products: []apishop.ProductResponse{{ProductID: "x"}},
		}
	}
	h := NewShopHandler(shopclient.New(fs.URL()))
	r := gin.New()
	r.Use(withPlayerID("p1"))
	r.GET("/products", h.GetProducts)

	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"product_id":"x"`)
}

func TestValidatePurchaseRequest(t *testing.T) {
	assert.NoError(t, validatePurchaseRequest(apigateway.PurchaseRequest{ProductID: "p", Platform: "ios", PurchaseToken: "t"}))
	assert.Error(t, validatePurchaseRequest(apigateway.PurchaseRequest{}))
}
