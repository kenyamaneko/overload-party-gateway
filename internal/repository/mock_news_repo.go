package repository

import (
	"context"
	"sync"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

type MockNewsRepository struct {
	mu       sync.Mutex
	articles []*model.NewsArticle
}

var _ port.NewsRepo = (*MockNewsRepository)(nil)

func NewMockNewsRepository() *MockNewsRepository {
	return &MockNewsRepository{}
}

func (r *MockNewsRepository) Seed(articles []*model.NewsArticle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.articles = articles
}

func (r *MockNewsRepository) List(ctx context.Context, limit, offset int) ([]*model.NewsArticle, error) {
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
