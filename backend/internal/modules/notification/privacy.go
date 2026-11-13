package notification

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Service) PurgeUser(ctx context.Context, tx pgx.Tx, user string) error {
	if _, err := tx.Exec(ctx, "DELETE FROM notification.deliveries WHERE notification_id IN (SELECT id FROM notification.notifications WHERE user_id=$1)", user); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM notification.notifications WHERE user_id=$1", user)
	return err
}
