package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/shopclient"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

type fakeShopServer struct {
	mu     sync.Mutex
	server *httptest.Server

	selectFactionStatus int
	selectFactionBody   any

	getProductsStatus int
	getProductsBody   any

	purchaseStatus int
	purchaseBody   any

	subscribeStatus int
	subscribeBody   any
}

func newFakeShopServer() *fakeShopServer {
	fs := &fakeShopServer{
		selectFactionStatus: http.StatusOK,
		selectFactionBody:   shopclient.SelectFactionResponse{Message: "ok", Faction: "f1", CardsGranted: 5},
		getProductsStatus:   http.StatusOK,
		purchaseStatus:      http.StatusOK,
		subscribeStatus:     http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/v1/players/", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/select-faction"):
			w.WriteHeader(fs.selectFactionStatus)
			if fs.selectFactionBody != nil {
				_ = json.NewEncoder(w).Encode(fs.selectFactionBody)
			}
		case strings.HasSuffix(path, "/products"):
			w.WriteHeader(fs.getProductsStatus)
			if fs.getProductsBody != nil {
				_ = json.NewEncoder(w).Encode(fs.getProductsBody)
			}
		case strings.HasSuffix(path, "/purchase"):
			w.WriteHeader(fs.purchaseStatus)
			if fs.purchaseBody != nil {
				_ = json.NewEncoder(w).Encode(fs.purchaseBody)
			}
		case strings.HasSuffix(path, "/subscribe"):
			w.WriteHeader(fs.subscribeStatus)
			if fs.subscribeBody != nil {
				_ = json.NewEncoder(w).Encode(fs.subscribeBody)
			}
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	})
	fs.server = httptest.NewServer(mux)
	return fs
}

func (f *fakeShopServer) close()                     { f.server.Close() }
func (f *fakeShopServer) client() *shopclient.Client { return shopclient.New(f.server.URL) }

func TestShopHandler_SelectFaction(t *testing.T) {
	t.Run("invalid JSON returns 400", func(t *testing.T) {
		fs := newFakeShopServer()
		defer fs.close()
		h := NewShopHandler(fs.client())
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
		fs := newFakeShopServer()
		defer fs.close()
		h := NewShopHandler(fs.client())
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
		fs := newFakeShopServer()
		defer fs.close()
		fs.selectFactionStatus = http.StatusConflict
		fs.selectFactionBody = map[string]string{"error": "faction already selected"}
		h := NewShopHandler(fs.client())
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
		fs := newFakeShopServer()
		defer fs.close()
		fs.selectFactionStatus = http.StatusBadRequest
		fs.selectFactionBody = map[string]string{"error": "invalid faction"}
		h := NewShopHandler(fs.client())
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
		fs := newFakeShopServer()
		defer fs.close()
		h := NewShopHandler(fs.client())
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
			fs := newFakeShopServer()
			defer fs.close()
			h := NewShopHandler(fs.client())
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
	fs := newFakeShopServer()
	defer fs.close()
	h := NewShopHandler(fs.client())
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
	fs := newFakeShopServer()
	defer fs.close()
	fs.purchaseStatus = http.StatusPaymentRequired
	fs.purchaseBody = map[string]string{"error": "invalid receipt"}
	h := NewShopHandler(fs.client())
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
	fs := newFakeShopServer()
	defer fs.close()
	exp := time.Now().Add(24 * time.Hour).UTC()
	fs.subscribeBody = map[string]any{"message": "ok", "expires_at": exp}
	h := NewShopHandler(fs.client())
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
	fs := newFakeShopServer()
	defer fs.close()
	h := NewShopHandler(fs.client())
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
	fs := newFakeShopServer()
	defer fs.close()
	fs.getProductsBody = map[string]any{"products": []map[string]any{{"product_id": "x"}}}
	h := NewShopHandler(fs.client())
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
