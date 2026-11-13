package media

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) PurgeSpace(ctx context.Context, tx pgx.Tx, sid string) error {
	return s.purge(ctx, tx, `(o.purpose='space' AND o.target_id=$1) OR
 (o.purpose='food' AND EXISTS(SELECT 1 FROM catalog.foods f WHERE f.id=o.target_id AND f.owner_space_id=$1)) OR
 (o.purpose='recipe' AND EXISTS(SELECT 1 FROM recipe.recipes r WHERE r.space_id=$1 AND (r.id=o.target_id OR r.image_object_id=o.id))
 AND NOT EXISTS(SELECT 1 FROM recipe.recipes r WHERE r.deleted_at IS NULL AND (r.id=o.target_id OR r.image_object_id=o.id) AND r.space_id IS DISTINCT FROM $1))`, sid)
}
func (s *Service) purge(ctx context.Context, tx pgx.Tx, predicate string, arg any) error {
	rows, err := tx.Query(ctx, "SELECT o.id::text,o.staging_key,o.object_key,o.expires_at FROM media.objects o WHERE ("+predicate+") FOR UPDATE OF o", arg)
	if err != nil {
		return err
	}
	ids, keys, staging := []string{}, []string{}, []string{}
	lastExpiry := s.db.Clock.Now()
	for rows.Next() {
		var id, stage string
		var object *string
		var expiry time.Time
		if err = rows.Scan(&id, &stage, &object, &expiry); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		staging = append(staging, stage)
		keys = append(keys, stage)
		if object != nil {
			keys = append(keys, *object)
		}
		if expiry.After(lastExpiry) {
			lastExpiry = expiry
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	key := platform.ID()
	if err = s.enqueueDelete(ctx, tx, key, keys, s.db.Clock.Now()); err != nil {
		return err
	}
	if lastExpiry.After(s.db.Clock.Now()) {
		if err = s.enqueueDelete(ctx, tx, key+":staging", staging, lastExpiry.Add(time.Second)); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "DELETE FROM media.objects WHERE id=ANY($1::uuid[])", ids)
	return err
}
func (s *Service) enqueueDelete(ctx context.Context, tx pgx.Tx, key string, keys []string, at time.Time) error {
	return s.db.Enqueue(ctx, tx, "media.delete", key, map[string]any{"keys": keys}, at)
}
func (s *Service) discard(ctx context.Context, key string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.db.Tx(cleanup, func(tx pgx.Tx) error { return s.enqueueDelete(cleanup, tx, key, []string{key}, s.db.Clock.Now()) })
}
func (s *Service) Handle(ctx context.Context, event platform.Event) error {
	switch event.Event {
	case "media.delete":
		var p struct {
			Keys []string `json:"keys"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		return s.store.Delete(ctx, p.Keys)
	case "account.anonymized":
		var p struct {
			UserID string `json:"userId"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		if p.UserID == "" {
			return errors.New("missing anonymized account")
		}
		return s.db.Tx(ctx, func(tx pgx.Tx) error {
			return s.purge(ctx, tx, `o.uploaded_by=$1 AND NOT (o.status='ready' AND (
   (o.purpose='space' AND EXISTS(SELECT 1 FROM household.spaces h WHERE h.id=o.target_id AND h.kind='family' AND h.dissolved_at IS NULL AND h.avatar_object_id=o.id)) OR
   (o.purpose='food' AND EXISTS(SELECT 1 FROM catalog.foods f LEFT JOIN household.spaces h ON h.id=f.owner_space_id WHERE f.id=o.target_id AND f.status!='inactive' AND (f.scope='global' OR (h.kind='family' AND h.dissolved_at IS NULL)))) OR
   (o.purpose='recipe' AND EXISTS(SELECT 1 FROM recipe.recipes r LEFT JOIN household.spaces h ON h.id=r.space_id WHERE r.image_object_id=o.id AND r.deleted_at IS NULL AND (r.kind='template' OR (h.kind='family' AND h.dissolved_at IS NULL))))
  ))`, p.UserID)
		})
	default:
		return errors.New("unsupported media event")
	}
}
