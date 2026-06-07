package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txContextKey struct{}

type queryExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WithTx stores tx in ctx so downstream repositories can join the same transaction.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// TxFromContext returns a transaction carried by ctx, if present.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	return tx, ok
}

// ExecutorFromContext returns the current transaction from ctx or falls back to pool.
func ExecutorFromContext(ctx context.Context, pool *pgxpool.Pool) queryExecutor {
	if tx, ok := TxFromContext(ctx); ok {
		return tx
	}
	return pool
}

// WithinTransaction runs fn inside a transaction and propagates it through ctx.
func WithinTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context) error) (err error) {
	if _, ok := TxFromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if err = fn(WithTx(ctx, tx)); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}
