package notification

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) Cleanup(ctx context.Context) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		now := s.db.Clock.Now()
		if _, err := tx.Exec(ctx, `DELETE FROM notification.deliveries d USING notification.notifications n WHERE d.notification_id=n.id AND (n.created_at<=$1 OR coalesce(d.sent_at,n.created_at)<=$2)`, now.Add(-90*24*time.Hour), now.Add(-30*24*time.Hour)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "DELETE FROM notification.notifications WHERE created_at<=$1", now.Add(-90*24*time.Hour))
		return err
	})
}
