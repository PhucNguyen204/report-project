package inventory

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Service) SearchLots(ctx context.Context, tx pgx.Tx, sid, q string) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, `SELECT l.id::text,l.food_id::text,f.name,l.remaining_qty::text,l.unit_id::text,to_char(l.expires_on,'YYYY-MM-DD'),l.storage_location FROM pantry.food_lots l JOIN catalog.foods f ON f.id=l.food_id WHERE l.space_id=$1 AND l.deleted_at IS NULL AND l.lifecycle='active' AND replace(unaccent(lower(f.name)),'đ','d') LIKE '%'||replace(unaccent(lower($2)),'đ','d')||'%' ORDER BY l.expires_on,l.id LIMIT 100`, sid, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, food, name, amount, unit, expiry, location string
		if err = rows.Scan(&id, &food, &name, &amount, &unit, &expiry, &location); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "foodId": food, "name": name, "quantity": amount, "unitId": unit, "expiresOn": expiry, "location": location})
	}
	return out, rows.Err()
}
