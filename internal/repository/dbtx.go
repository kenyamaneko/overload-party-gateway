package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Compile-time interface check.
var _ port.TxRunner = (*TxManager)(nil)

// dbtx is the common subset of pgxpool.Pool and pgx.Tx used by repository methods.
type dbtx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txKey is the context key used to propagate a transaction.
type txKey struct{}

// txFromContext returns the transaction stored in the context, or nil.
func txFromContext(ctx context.Context) dbtx {
	if tx, ok := ctx.Value(txKey{}).(dbtx); ok {
		return tx
	}
	return nil
}

// connFrom returns the transaction from the context if present, otherwise the pool.
func connFrom(ctx context.Context, pool *pgxpool.Pool) dbtx {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return pool
}

// TxManager implements port.TxRunner using a pgxpool.Pool.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// MockTxRunner implements port.TxRunner for tests. No real transaction is started.
type MockTxRunner struct{}

func (m *MockTxRunner) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
