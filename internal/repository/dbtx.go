package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is the common subset of pgxpool.Pool and pgx.Tx used by repository methods.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxRunner provides service-level transaction control.
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(tx DBTX) error) error
}

// TxManager implements TxRunner using a pgxpool.Pool.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) RunInTx(ctx context.Context, fn func(tx DBTX) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// MockTxRunner implements TxRunner for tests. DBTX is nil since mock repos ignore it.
type MockTxRunner struct{}

func (m *MockTxRunner) RunInTx(_ context.Context, fn func(tx DBTX) error) error {
	return fn(nil)
}
