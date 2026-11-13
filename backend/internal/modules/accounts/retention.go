package accounts

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) Cleanup(ctx context.Context) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		now := s.db.Clock.Now()
		if _, err := tx.Exec(ctx, `DELETE FROM identity.otp_challenges WHERE GREATEST(expires_at,reset_expires_at)<=$1`, now.Add(-24*time.Hour)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM identity.refresh_sessions WHERE COALESCE(revoked_at,expires_at)<=$1`, now.Add(-30*24*time.Hour))
		return err
	})
}
