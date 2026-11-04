// Package repository implements persistence on top of sqlc generated code.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buildoshi/e2b-api/internal/repository/sqlcgen"
	"github.com/buildoshi/e2b-api/internal/usecase"
	"github.com/buildoshi/e2b-api/pkg/retry"
)

// Store is the Postgres backed implementation of usecase.Store.
type Store struct {
	queries
	pool *pgxpool.Pool
}

var _ usecase.Store = (*Store)(nil)

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: queries{q: sqlcgen.New(pool)}, pool: pool}
}

// InTx runs fn in a transaction and commits when it returns nil. Transient
// failures (deadlock, serialization failure) rerun fn from the start, so fn
// must not have side effects outside the database.
func (s *Store) InTx(ctx context.Context, fn func(tx usecase.Repository) error) error {
	return retry.Do(ctx, txRetry, isTransient, func(ctx context.Context) error {
		return s.runTx(ctx, fn)
	})
}

func (s *Store) runTx(ctx context.Context, fn func(tx usecase.Repository) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Rollback after a successful commit is a harmless no-op.
		if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
	}()

	if err := fn(queries{q: sqlcgen.New(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func isTransient(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case pgSerializationFailure, pgDeadlockDetected:
		return true
	default:
		return false
	}
}
