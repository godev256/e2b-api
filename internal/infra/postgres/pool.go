// Package postgres creates the connection pool and runs schema changes.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buildoshi/e2b-api/pkg/retry"
)

// PoolConfig tunes the connection pool.
type PoolConfig struct {
	MaxConns         int32
	MinConns         int32
	MaxConnLifetime  time.Duration
	MaxConnIdleTime  time.Duration
	HealthCheckEvery time.Duration
	// StatementTimeout makes Postgres cancel any single statement that runs
	// longer, so a slow query cannot pin a connection forever.
	StatementTimeout time.Duration
	// LockTimeout bounds the wait for a row lock, e.g. the project lock taken
	// when creating a sandbox.
	LockTimeout time.Duration
}

// startupRetry gives the database time to come up in docker compose.
var startupRetry = retry.Config{MaxAttempts: 10, BaseDelay: 200 * time.Millisecond, MaxDelay: 2 * time.Second}

// NewPool opens a pool and verifies it can reach the database, retrying while
// the database is still starting.
func NewPool(ctx context.Context, url string, cfg PoolConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.HealthCheckEvery
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprint(cfg.StatementTimeout.Milliseconds())
	poolCfg.ConnConfig.RuntimeParams["lock_timeout"] = fmt.Sprint(cfg.LockTimeout.Milliseconds())
	poolCfg.ConnConfig.RuntimeParams["application_name"] = "e2b-api"

	return retry.Value(ctx, startupRetry, func(error) bool { return true }, func(ctx context.Context) (*pgxpool.Pool, error) {
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			return nil, fmt.Errorf("create pool: %w", err)
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, fmt.Errorf("ping database: %w", err)
		}
		return pool, nil
	})
}
