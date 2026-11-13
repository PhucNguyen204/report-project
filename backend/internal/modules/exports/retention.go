package exports

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Cleanup(ctx context.Context) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error { return s.purge(ctx, tx, "expires_at<=$1", s.db.Clock.Now()) })
}
