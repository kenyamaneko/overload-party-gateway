package rest

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// NewsHandler はクラウドニュースの REST エンドポイントを処理します
type NewsHandler struct {
	svc *service.NewsService
}

// NewNewsHandler は NewsHandler を生成します
func NewNewsHandler(svc *service.NewsService) *NewsHandler {
	return &NewsHandler{svc: svc}
}

// GetCloudNews はクラウドニュース記事一覧を返します
func (h *NewsHandler) GetCloudNews(c *gin.Context) {
	limit := 20
	offset := 0

	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	articles, err := h.svc.List(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch news"})
		return
	}

	if articles == nil {
		c.JSON(http.StatusOK, []any{})
		return
	}

	c.JSON(http.StatusOK, articles)
}
