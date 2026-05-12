package rest_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
)

// withInternalAuth は IssueInternalAuth middleware を模した helper。
// 任意の token を c.Request.Context に格納し、forwarder の Director が
// header に転載できる状態を作る。
func withInternalAuth(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := internalauth.WithToken(c.Request.Context(), token)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// capturedRequest は backend が受け取ったリクエストの観測値。
type capturedRequest struct {
	method string
	path   string
	query  string
	body   string
	auth   string
}

// newCapturingBackend は受信した request を *capturedRequest に記録する
// httptest.Server を返す。指定の status / body で応答する。
func newCapturingBackend(t *testing.T, status int, body string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.query = r.URL.RawQuery
		bodyBytes, _ := io.ReadAll(r.Body)
		got.body = string(bodyBytes)
		got.auth = r.Header.Get(internalauth.HeaderName)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// TestNewForwarder は forwarder の振る舞いを method / body / query / status /
// auth header の各観点で検証する。
func TestNewForwarder(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name           string
		token          string
		method         string
		reqPath        string
		reqBody        string
		backendStatus  int
		backendBody    string
		wantBackend    capturedRequest
		wantClientCode int
		wantClientBody string
	}{
		{
			name:          "GET 透過 + X-Internal-Auth 注入",
			token:         "test-token-abc",
			method:        http.MethodGet,
			reqPath:       "/api/v1/account/me",
			reqBody:       "",
			backendStatus: http.StatusOK,
			backendBody:   `{"player_id":"p1"}`,
			wantBackend: capturedRequest{
				method: http.MethodGet,
				path:   "/api/v1/account/me",
				query:  "",
				body:   "",
				auth:   "test-token-abc",
			},
			wantClientCode: http.StatusOK,
			wantClientBody: `{"player_id":"p1"}`,
		},
		{
			name:          "POST は body と query を保持",
			token:         "token-2",
			method:        http.MethodPost,
			reqPath:       "/api/v1/shop/purchase?platform=ios",
			reqBody:       `{"product_id":"x"}`,
			backendStatus: http.StatusCreated,
			backendBody:   `{"ok":true}`,
			wantBackend: capturedRequest{
				method: http.MethodPost,
				path:   "/api/v1/shop/purchase",
				query:  "platform=ios",
				body:   `{"product_id":"x"}`,
				auth:   "token-2",
			},
			wantClientCode: http.StatusCreated,
			wantClientBody: `{"ok":true}`,
		},
		{
			name:          "PUT も passthrough",
			token:         "token-put",
			method:        http.MethodPut,
			reqPath:       "/api/v1/cards/decks/1",
			reqBody:       `{"deck_name":"d"}`,
			backendStatus: http.StatusOK,
			backendBody:   `{"deck_id":1}`,
			wantBackend: capturedRequest{
				method: http.MethodPut,
				path:   "/api/v1/cards/decks/1",
				query:  "",
				body:   `{"deck_name":"d"}`,
				auth:   "token-put",
			},
			wantClientCode: http.StatusOK,
			wantClientBody: `{"deck_id":1}`,
		},
		{
			name:          "DELETE も passthrough",
			token:         "token-del",
			method:        http.MethodDelete,
			reqPath:       "/api/v1/cards/decks/1",
			reqBody:       "",
			backendStatus: http.StatusNoContent,
			backendBody:   "",
			wantBackend: capturedRequest{
				method: http.MethodDelete,
				path:   "/api/v1/cards/decks/1",
				query:  "",
				body:   "",
				auth:   "token-del",
			},
			wantClientCode: http.StatusNoContent,
			wantClientBody: "",
		},
		{
			name:          "downstream 404 が透過する",
			token:         "token-3",
			method:        http.MethodGet,
			reqPath:       "/api/v1/cards/decks/999",
			reqBody:       "",
			backendStatus: http.StatusNotFound,
			backendBody:   `{"error":"deck not found"}`,
			wantBackend: capturedRequest{
				method: http.MethodGet,
				path:   "/api/v1/cards/decks/999",
				query:  "",
				body:   "",
				auth:   "token-3",
			},
			wantClientCode: http.StatusNotFound,
			wantClientBody: `{"error":"deck not found"}`,
		},
		{
			name:          "downstream 500 が透過する",
			token:         "token-5xx",
			method:        http.MethodGet,
			reqPath:       "/api/v1/news/articles",
			reqBody:       "",
			backendStatus: http.StatusInternalServerError,
			backendBody:   `{"error":"db down"}`,
			wantBackend: capturedRequest{
				method: http.MethodGet,
				path:   "/api/v1/news/articles",
				query:  "",
				body:   "",
				auth:   "token-5xx",
			},
			wantClientCode: http.StatusInternalServerError,
			wantClientBody: `{"error":"db down"}`,
		},
		{
			name:          "internal auth token なし = header 空",
			token:         "",
			method:        http.MethodGet,
			reqPath:       "/api/v1/news/articles",
			reqBody:       "",
			backendStatus: http.StatusOK,
			backendBody:   `[]`,
			wantBackend: capturedRequest{
				method: http.MethodGet,
				path:   "/api/v1/news/articles",
				query:  "",
				body:   "",
				auth:   "",
			},
			wantClientCode: http.StatusOK,
			wantClientBody: `[]`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			backend, got := newCapturingBackend(t, tc.backendStatus, tc.backendBody)

			fwd, err := rest.NewForwarder(backend.URL)
			require.NoError(t, err)

			r := gin.New()
			r.Use(withInternalAuth(tc.token))
			r.Any("/api/v1/*path", fwd)

			// ReverseProxy 内部で CloseNotify を呼ぶため、httptest.NewRecorder
			// (CloseNotifier 未実装) ではなく httptest.NewServer を使って実 HTTP
			// セマンティクスで叩く。
			gw := httptest.NewServer(r)
			defer gw.Close()

			req, err := http.NewRequest(tc.method, gw.URL+tc.reqPath, strings.NewReader(tc.reqBody))
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)

			assert.Equal(t, tc.wantBackend, *got)
			assert.Equal(t, tc.wantClientCode, resp.StatusCode)
			assert.Equal(t, tc.wantClientBody, string(respBody))
		})
	}
}

// 不正な targetURL は err を返す
func TestNewForwarder_InvalidURL(t *testing.T) {
	_, err := rest.NewForwarder("://not-a-url")
	require.Error(t, err)
}
