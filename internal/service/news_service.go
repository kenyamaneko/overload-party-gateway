package service

import (
	"context"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

type NewsService struct {
	repo port.NewsRepo
}

func NewNewsService(repo port.NewsRepo) *NewsService {
	return &NewsService{repo: repo}
}

func (s *NewsService) List(ctx context.Context, limit, offset int) ([]*model.NewsArticle, error) {
	return s.repo.List(ctx, limit, offset)
}
