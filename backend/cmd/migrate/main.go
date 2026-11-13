package main

import (
	"context"
	"dicho/internal/platform"
	"dicho/migrations"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"os"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := run(ctx, os.Getenv); err != nil {
		slog.Error("database migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("database migrations verified")
}

func run(ctx context.Context, get func(string) string) error {
	dsn := get("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("database connection failed")
	}
	if err := platform.Migrate(ctx, pool, migrations.Files); err != nil {
		return err
	}
	if err := platform.CheckMigrations(ctx, pool, migrations.Files); err != nil {
		return err
	}
	return platform.MigrateQueue(ctx, pool)
}
