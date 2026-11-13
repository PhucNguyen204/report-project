package platform

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ContextPool struct{ *pgxpool.Pool }

func WithTransaction(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, transactionKey{}, tx)
}

func (p *ContextPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx, ok := ctx.Value(transactionKey{}).(pgx.Tx); ok {
		return tx.Exec(ctx, sql, args...)
	}
	return p.Pool.Exec(ctx, sql, args...)
}
func (p *ContextPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx, ok := ctx.Value(transactionKey{}).(pgx.Tx); ok {
		return tx.Query(ctx, sql, args...)
	}
	return p.Pool.Query(ctx, sql, args...)
}
func (p *ContextPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx, ok := ctx.Value(transactionKey{}).(pgx.Tx); ok {
		return tx.QueryRow(ctx, sql, args...)
	}
	return p.Pool.QueryRow(ctx, sql, args...)
}
