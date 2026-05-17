package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
)

// hitRecorder は backend が何度叩かれたかを記録する httptest.Server を作る helper。
type hitRecorder struct {
	srv  *httptest.Server
	hits int
	auth string
	path string
}

func newHitRecorder(t *testing.T) *hitRecorder {
	t.Helper()
	rec := &hitRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.hits++
		rec.auth = r.Header.Get(internalauth.HeaderName)
		rec.path = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, r.Body)
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

// TestRegisterForwardRoutes は各 path prefix が正しい backend にルーティングされる
// ことを 7 サービス (account / card / shop / scenario / news / support / battle)
// 全網羅で検証する。
func TestRegisterForwardRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := newHitRecorder(t)
	card := newHitRecorder(t)
	shop := newHitRecorder(t)
	scenario := newHitRecorder(t)
	news := newHitRecorder(t)
	support := newHitRecorder(t)
	battle := newHitRecorder(t)

	cfg := &config.Config{
		AccountServiceURL:  account.srv.URL,
		CardServiceURL:     card.srv.URL,
		ShopServiceURL:     shop.srv.URL,
		ScenarioServiceURL: scenario.srv.URL,
		NewsServiceURL:     news.srv.URL,
		SupportServiceURL:  support.srv.URL,
		BattleServerURL:    battle.srv.URL,
	}

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		ctx := internalauth.WithToken(c.Request.Context(), "test-jwt")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	require.NoError(t, router.RegisterForwardRoutes(api, cfg))

	// 各 prefix → 期待 backend のテーブル。/games/* と /npc/* はいずれも battle に向く。
	cases := []struct {
		name        string
		path        string
		wantBackend *hitRecorder
	}{
		{name: "/api/v1/account/me → account", path: "/api/v1/account/me", wantBackend: account},
		{name: "/api/v1/cards/cards → card", path: "/api/v1/cards/cards", wantBackend: card},
		{name: "/api/v1/cards/decks/1 → card", path: "/api/v1/cards/decks/1", wantBackend: card},
		{name: "/api/v1/shop/products → shop", path: "/api/v1/shop/products", wantBackend: shop},
		{name: "/api/v1/scenarios/episodes → scenario", path: "/api/v1/scenarios/episodes", wantBackend: scenario},
		{name: "/api/v1/scenarios/onboarding/script → scenario", path: "/api/v1/scenarios/onboarding/script", wantBackend: scenario},
		{name: "/api/v1/news/articles → news", path: "/api/v1/news/articles", wantBackend: news},
		{name: "/api/v1/support/announcements → support", path: "/api/v1/support/announcements", wantBackend: support},
		{name: "/api/v1/games/g1/log → battle", path: "/api/v1/games/g1/log", wantBackend: battle},
		{name: "/api/v1/games/g1/log/text → battle", path: "/api/v1/games/g1/log/text", wantBackend: battle},
		{name: "/api/v1/npc/models → battle", path: "/api/v1/npc/models", wantBackend: battle},
	}

	// ReverseProxy 内部で CloseNotify を呼ぶため、gateway 側も httptest.NewServer
	// で実 HTTP セマンティクスで叩く。
	gw := httptest.NewServer(r)
	defer gw.Close()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			beforeHits := tc.wantBackend.hits
			resp, err := http.Get(gw.URL + tc.path)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, beforeHits+1, tc.wantBackend.hits, "期待 backend が呼ばれていない")
			assert.Equal(t, "test-jwt", tc.wantBackend.auth, "X-Internal-Auth header が backend に届いていない")
			assert.Equal(t, tc.path, tc.wantBackend.path, "path がそのまま透過していない")
		})
	}
}

// 不正な targetURL を含む config は err を返す
func TestRegisterForwardRoutes_InvalidURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		AccountServiceURL:  "http://valid.example",
		CardServiceURL:     "http://valid.example",
		ShopServiceURL:     "http://valid.example",
		ScenarioServiceURL: "http://valid.example",
		NewsServiceURL:     "://invalid",
		SupportServiceURL:  "http://valid.example",
		BattleServerURL:    "http://valid.example",
	}

	r := gin.New()
	api := r.Group("/api/v1")
	err := router.RegisterForwardRoutes(api, cfg)
	require.Error(t, err)
}
