package router

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
)

// Handlers は gateway 残置の REST ハンドラーをグループ化し、cmd/main と cmd/local
// でルート定義を共有します。downstream サービス公開 path は RegisterForwardRoutes で
// 別途扱う。
type Handlers struct {
	Auth *rest.AuthHandler
}

// RegisterAuthRoutes は認証エンドポイント（register / login）を登録します
func RegisterAuthRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/auth/register", h.Auth.Register)
	group.POST("/auth/login", h.Auth.Login)
}

// RegisterForwardRoutes は各サービスへの path-prefix forwarder を登録します。
// gateway は path の prefix で振り分けるだけで、固有のドメインロジックや型加工は持たない。
// 認証 / プレイヤー解決 / 内部 JWT 発行は本関数呼び出し前の middleware で完了している前提。
func RegisterForwardRoutes(api *gin.RouterGroup, cfg *config.Config) error {
	routes := []struct {
		pathPattern string
		targetURL   string
	}{
		{"/account/*path", cfg.AccountServiceURL},
		{"/cards/*path", cfg.CardServiceURL},
		{"/shop/*path", cfg.ShopServiceURL},
		{"/scenarios/*path", cfg.ScenarioServiceURL},
		{"/news/*path", cfg.NewsServiceURL},
		{"/support/*path", cfg.SupportServiceURL},
		{"/games/*path", cfg.BattleServerURL},
		{"/npc/*path", cfg.BattleServerURL},
	}
	for _, r := range routes {
		h, err := rest.NewForwarder(r.targetURL)
		if err != nil {
			return fmt.Errorf("forwarder for %q: %w", r.pathPattern, err)
		}
		api.Any(r.pathPattern, h)
	}
	return nil
}
