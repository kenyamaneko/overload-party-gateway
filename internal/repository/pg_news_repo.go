package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.NewsRepo = (*PgNewsRepository)(nil)

// PgNewsRepository は PostgreSQL によるニュース記事のリポジトリです
type PgNewsRepository struct {
	pool *pgxpool.Pool
}

// NewPgNewsRepository は PgNewsRepository を生成します
func NewPgNewsRepository(pool *pgxpool.Pool) *PgNewsRepository {
	return &PgNewsRepository{pool: pool}
}

// List はニュース記事を公開日降順で取得します
func (r *PgNewsRepository) List(ctx context.Context, limit int, offset int) ([]*apigateway.NewsArticle, error) {
	rows, err := connFrom(ctx, r.pool).Query(ctx, `
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

	var articles []*apigateway.NewsArticle
	for rows.Next() {
		a := &apigateway.NewsArticle{}
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
