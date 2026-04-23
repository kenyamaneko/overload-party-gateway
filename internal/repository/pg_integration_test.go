//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connStr := os.Getenv("TEST_DB_URL")
	if connStr == "" {
		t.Skip("TEST_DB_URL not set; run: docker compose -f ../overload-party-common/db/docker-compose.test.yml up -d")
	}

	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	tables := []string{"news_articles", "gateway.game_players"}
	for _, table := range tables {
		_, err := pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE %s CASCADE", table))
		require.NoError(t, err)
	}

	return pool
}

func TestPgNews_List(t *testing.T) {
	pool := setupPool(t)
	repo := NewPgNewsRepository(pool)
	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 5; i++ {
		summary := fmt.Sprintf("Summary %d", i)
		_, err := pool.Exec(ctx,
			`INSERT INTO news_articles (article_id, source, source_url, title, summary, tags, published_at, fetched_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			fmt.Sprintf("article-%d", i),
			"aws",
			fmt.Sprintf("https://example.com/article-%d", i),
			fmt.Sprintf("Title %d", i),
			&summary,
			[]string{"tag1"},
			now.Add(time.Duration(-i)*time.Hour),
			now,
		)
		require.NoError(t, err)
	}

	articles, err := repo.List(ctx, 3, 0)
	require.NoError(t, err)
	assert.Len(t, articles, 3)

	articles2, err := repo.List(ctx, 10, 3)
	require.NoError(t, err)
	assert.Len(t, articles2, 2)

	empty, err := repo.List(ctx, 10, 10)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
