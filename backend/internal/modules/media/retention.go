package media

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Cleanup(ctx context.Context) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		now := s.db.Clock.Now()
		if err := s.purge(ctx, tx, `(o.status='pending' AND o.expires_at<=$1) OR
  (o.status='ready' AND o.completed_at<=$1::timestamptz-interval '30 days' AND NOT (
   (o.purpose='avatar' AND EXISTS(SELECT 1 FROM identity.users u WHERE u.avatar_object_id=o.id AND u.deleted_at IS NULL)) OR
   (o.purpose='space' AND EXISTS(SELECT 1 FROM household.spaces h WHERE h.avatar_object_id=o.id AND h.dissolved_at IS NULL)) OR
   (o.purpose='recipe' AND EXISTS(SELECT 1 FROM recipe.recipes r WHERE r.image_object_id=o.id AND r.deleted_at IS NULL)) OR
   (o.purpose='food' AND EXISTS(SELECT 1 FROM catalog.foods f WHERE f.id=o.target_id AND f.status!='inactive'))
  ))`, now); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT id::text,staging_key FROM media.objects WHERE status='ready' AND expires_at<=$1 AND staging_cleanup_at IS NULL ORDER BY id LIMIT 500 FOR UPDATE SKIP LOCKED", now)
		if err != nil {
			return err
		}
		ids, keys := []string{}, []string{}
		for rows.Next() {
			var id, key string
			if err = rows.Scan(&id, &key); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
			keys = append(keys, key)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err = s.enqueueDelete(ctx, tx, "staging:"+platform.ID(), keys, now); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE media.objects SET staging_cleanup_at=$2 WHERE id=ANY($1::uuid[])", ids, now)
		return err
	})
}
