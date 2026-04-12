package repository

import (
	"context"
	"sync"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// MockNewsRepository はテスト用のインメモリ NewsRepo 実装です
type MockNewsRepository struct {
	mu       sync.Mutex
	articles []*apigateway.NewsArticle
}

var _ port.NewsRepo = (*MockNewsRepository)(nil)

// NewMockNewsRepository は MockNewsRepository を生成します
func NewMockNewsRepository() *MockNewsRepository {
	return &MockNewsRepository{}
}

// Seed はテスト用の記事データを設定します
func (r *MockNewsRepository) Seed(articles []*apigateway.NewsArticle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.articles = articles
}

// List はテスト用の記事一覧を返します
func (r *MockNewsRepository) List(ctx context.Context, limit, offset int) ([]*apigateway.NewsArticle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if offset >= len(r.articles) {
		return nil, nil
	}
	end := offset + limit
	if end > len(r.articles) {
		end = len(r.articles)
	}
	return r.articles[offset:end], nil
}
