package accounts

import (
	"context"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Service) lockAdministrator(ctx context.Context, tx pgx.Tx, actor string) error {
	var allowed bool
	err := tx.QueryRow(ctx, "SELECT true FROM identity.users WHERE id=$1 AND system_role='admin' AND status='active' AND deleted_at IS NULL FOR SHARE", actor).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
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
