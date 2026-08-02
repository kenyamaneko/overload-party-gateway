package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"cloud.google.com/go/cloudsqlconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newDatabasePool は IAM 認証設定に応じた接続方式で DB 接続プールを構築する。
func newDatabasePool(ctx context.Context, databaseConn string, iamAuthEnabled bool, cloudSQLConnectionName string) (*pgxpool.Pool, func(), error) {
	if !iamAuthEnabled {
		pool, err := pgxpool.New(ctx, databaseConn)
		if err != nil {
			return nil, nil, fmt.Errorf("pgxpool new: %w", err)
		}
		return pool, func() {}, nil
	}
	return newDatabasePoolWithIAMAuth(ctx, databaseConn, cloudSQLConnectionName)
}

func newDatabasePoolWithIAMAuth(ctx context.Context, databaseConn, cloudSQLConnectionName string) (*pgxpool.Pool, func(), error) {
	poolCfg, err := pgxpool.ParseConfig(databaseConn)
	if err != nil {
		return nil, nil, fmt.Errorf("parse database conn: %w", err)
	}

	dialer, err := cloudsqlconn.NewDialer(ctx,
		cloudsqlconn.WithIAMAuthN(),
		cloudsqlconn.WithDefaultDialOptions(cloudsqlconn.WithPrivateIP()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("cloudsqlconn new dialer: %w", err)
	}

	poolCfg.ConnConfig.DialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.Dial(ctx, cloudSQLConnectionName)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		closeDialer(dialer)
		return nil, nil, fmt.Errorf("pgxpool new with config: %w", err)
	}

	return pool, func() { closeDialer(dialer) }, nil
}

func closeDialer(dialer *cloudsqlconn.Dialer) {
	// 呼び出し元がプロセス終了処理の中で呼ぶため、失敗をログにとどめて処理を続行する。
	if err := dialer.Close(); err != nil {
		slog.Error("cloudsqlconn dialer close failed", "error", err)
	}
}
