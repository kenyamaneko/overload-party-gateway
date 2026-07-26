// Package postgrestest は DB を用いるテスト全般のヘルパを提供する。
// Testcontainers でコンテナを 1 回起動し (RunMain)、テスト関数間の状態リセットは Truncate で行う。
package postgrestest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Postgres は起動済み PostgreSQL コンテナと接続プールのハンドル。
type Postgres struct {
	Container *postgres.PostgresContainer
	Pool      *pgxpool.Pool
	schemas   []string
}

// Start は postgres:16-alpine コンテナを起動し db/schema.sql を適用した接続プールを返す。
// 終了時は Close を呼ぶ (RunMain が代行する)。
func Start(ctx context.Context) (*Postgres, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("container connection string: %w", err), container.Terminate(ctx))
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("pgxpool new: %w", err), container.Terminate(ctx))
	}

	schemaPath := filepath.Join(root, "db", "schema.sql")
	data, rerr := os.ReadFile(schemaPath)
	if rerr != nil {
		pool.Close()
		return nil, errors.Join(fmt.Errorf("read schema %s: %w", schemaPath, rerr), container.Terminate(ctx))
	}
	if _, eerr := pool.Exec(ctx, string(data)); eerr != nil {
		pool.Close()
		return nil, errors.Join(fmt.Errorf("apply schema %s: %w", schemaPath, eerr), container.Terminate(ctx))
	}

	return &Postgres{Container: container, Pool: pool, schemas: []string{"gateway"}}, nil
}

// Close は pool をクローズしコンテナを終了する。
func (p *Postgres) Close(ctx context.Context) error {
	p.Pool.Close()
	if err := p.Container.Terminate(ctx); err != nil {
		return fmt.Errorf("terminate container: %w", err)
	}
	return nil
}

// Truncate は gateway スキーマ配下の全 BASE TABLE を動的に TRUNCATE する。
// テーブル追加時にヘルパ更新を強いられないよう、information_schema で列挙する。
func (p *Postgres) Truncate(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	rows, err := p.Pool.Query(ctx, `
		SELECT table_schema || '.' || table_name
		  FROM information_schema.tables
		 WHERE table_schema = ANY($1)
		   AND table_type = 'BASE TABLE'
	`, p.schemas)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if serr := rows.Scan(&name); serr != nil {
			t.Fatalf("scan table name: %v", serr)
		}
		tables = append(tables, name)
	}
	if rerr := rows.Err(); rerr != nil {
		t.Fatalf("iterate tables: %v", rerr)
	}
	if len(tables) == 0 {
		return
	}

	stmt := "TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := p.Pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// RunMain は TestMain のボイラープレートを集約する。
func RunMain(m *testing.M, out **Postgres) int {
	ctx := context.Background()

	pg, err := Start(ctx)
	if err != nil {
		log.Fatalf("postgrestest.Start: %v", err)
	}
	*out = pg

	defer func() {
		if cerr := pg.Close(ctx); cerr != nil {
			log.Printf("postgrestest.Close: %v", cerr)
		}
	}()

	return m.Run()
}

// repoRoot は本ファイルの位置から go.mod を持つディレクトリを探索して返す。
func repoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}
