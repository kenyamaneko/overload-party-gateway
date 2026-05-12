package rest

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// NewForwarder は targetURL に raw forward する gin handler を返す。
// 各リクエストには IssueInternalAuth middleware が context に格納した内部認証 JWT を
// X-Internal-Auth ヘッダとして付与してから downstream へ送る。
func NewForwarder(targetURL string) (gin.HandlerFunc, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("forwarder: parse targetURL %q: %w", targetURL, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		internalauth.InjectHeader(req.Context(), req.Header)
	}
	return func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}, nil
}
