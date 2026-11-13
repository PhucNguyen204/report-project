package inventory

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func overwrittenFields(ctx context.Context, tx pgx.Tx, table, id string, base, current int64, patch, state map[string]any) ([]string, error) {
	result := []string{}
	if base > current {
		return nil, platform.Conflict()
	}
	if base == current || len(patch) == 0 {
		return result, nil
	}
	desired, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	previous, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(array_agg(p.key ORDER BY p.key),ARRAY[]::text[]) FROM `+table+` r CROSS JOIN LATERAL jsonb_each($2::jsonb) p WHERE r.id=$1 AND COALESCE((r.field_versions->>p.key)::bigint,1)>$3 AND p.value IS DISTINCT FROM ($4::jsonb->p.key)`, id, desired, base, previous).Scan(&result)
	return result, err
}
