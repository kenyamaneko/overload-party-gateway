package service

import (
	"context"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// NewsService はニュース記事の取得ロジックを提供します
type NewsService struct {
	repo port.NewsRepo
}

// NewNewsService は NewsService を生成します
func NewNewsService(repo port.NewsRepo) *NewsService {
	return &NewsService{repo: repo}
}

// List はニュース記事一覧を取得します
func (s *NewsService) List(ctx context.Context, limit, offset int) ([]*apigateway.NewsArticle, error) {
	return s.repo.List(ctx, limit, offset)
}
