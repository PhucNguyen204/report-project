package accounts

import (
	"context"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Service) lockAdministrator(ctx context.Context, tx pgx.Tx, actor string) error {
	role, err := s.db.LockActor(ctx, tx, actor)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "admin") {
		return platform.Forbidden()
	}
	return err
}
func (s *Service) adminQuery(ctx context.Context, actor, sql string, args ...any) ([]map[string]any, error) {
	var out []map[string]any
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if err := s.lockAdministrator(ctx, tx, actor); err != nil {
			return err
		}
		var err error
		out, err = queryMaps(ctx, tx, sql, args...)
		return err
	})
	return out, err
}
