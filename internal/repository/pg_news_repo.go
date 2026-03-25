package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Compile-time interface check.
var _ port.NewsRepo = (*PgNewsRepository)(nil)

type PgNewsRepository struct {
	pool *pgxpool.Pool
}

func NewPgNewsRepository(pool *pgxpool.Pool) *PgNewsRepository {
	return &PgNewsRepository{pool: pool}
}

func (r *PgNewsRepository) List(ctx context.Context, limit int, offset int) ([]*model.NewsArticle, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT article_id, source, title, summary, tags, published_at, fetched_at
		  FROM news_articles
		 WHERE summary IS NOT NULL
		 ORDER BY published_at DESC NULLS LAST
		 LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("pg_news_repo.List: %w", err)
	}
	defer rows.Close()

	var articles []*model.NewsArticle
	for rows.Next() {
		a := &model.NewsArticle{}
		if err := rows.Scan(
			&a.ArticleID, &a.Source, &a.Title,
			&a.Summary, &a.Tags, &a.PublishedAt, &a.FetchedAt,
		); err != nil {
			return nil, fmt.Errorf("pg_news_repo.List scan: %w", err)
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}
