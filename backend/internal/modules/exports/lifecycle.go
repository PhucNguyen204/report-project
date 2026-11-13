package exports

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) enqueueDelete(ctx context.Context, tx pgx.Tx, key string, keys []string) error {
	return s.db.Enqueue(ctx, tx, "export.delete", key, map[string]any{"keys": keys}, s.db.Clock.Now())
}
func (s *Service) discard(ctx context.Context, key string) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.db.Tx(cleanup, func(tx pgx.Tx) error { return s.enqueueDelete(cleanup, tx, key, []string{key}) })
}
func (s *Service) PurgeSpace(ctx context.Context, tx pgx.Tx, sid string) error {
	return s.purge(ctx, tx, "space_id=$1", sid)
}
func (s *Service) purge(ctx context.Context, tx pgx.Tx, predicate string, arg any) error {
	rows, err := tx.Query(ctx, "SELECT id::text,object_key FROM report.exports WHERE "+predicate+" FOR UPDATE", arg)
	if err != nil {
		return err
	}
	ids, keys := []string{}, []string{}
	for rows.Next() {
		var id string
		var key *string
		if err = rows.Scan(&id, &key); err != nil {
			rows.Close()
			return err
		}
		if key != nil {
			keys = append(keys, *key)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if len(keys) > 0 {
		if err = s.enqueueDelete(ctx, tx, platform.ID(), keys); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "DELETE FROM report.exports WHERE id=ANY($1::uuid[])", ids)
	return err
}
