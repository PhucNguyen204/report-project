package platform

import (
	"context"
	"dicho/internal/observability"
	"errors"
	"time"
)

func (db *DB) QueueStats(ctx context.Context) (observability.QueueSnapshot, error) {
	unavailable := errors.New("queue statistics unavailable")
	if db == nil || db.Pool == nil || db.Pool.Pool == nil || db.Clock == nil {
		return observability.QueueSnapshot{}, unavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	rows, err := db.Pool.Pool.Query(ctx, `SELECT CASE WHEN args->>'event'=ANY($2::text[]) THEN args->>'event' ELSE 'other' END,
 state::text,count(*),COALESCE(max(greatest(extract(epoch FROM ($1::timestamptz-scheduled_at)),0)) FILTER (WHERE state IN ('available','retryable','scheduled') AND scheduled_at<=$1),0)::float8
 FROM river_job WHERE state IN ('available','pending','retryable','running','scheduled','discarded') GROUP BY 1,2`, db.Clock.Now(), observability.JobKinds())
	if err != nil {
		if ctx.Err() != nil {
			return observability.QueueSnapshot{}, ctx.Err()
		}
		return observability.QueueSnapshot{}, unavailable
	}
	defer rows.Close()
	out := observability.QueueSnapshot{Groups: []observability.QueueGroup{}}
	for rows.Next() {
		var group observability.QueueGroup
		if err := rows.Scan(&group.Kind, &group.State, &group.Jobs, &group.OldestDueSeconds); err != nil {
			return observability.QueueSnapshot{}, unavailable
		}
		out.Groups = append(out.Groups, group)
	}
	if err := rows.Err(); err != nil {
		if ctx.Err() != nil {
			return observability.QueueSnapshot{}, ctx.Err()
		}
		return observability.QueueSnapshot{}, unavailable
	}
	return out, nil
}
