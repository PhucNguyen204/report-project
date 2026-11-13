package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func (s *Service) CopySpace(ctx context.Context, tx pgx.Tx, source, target, user string, copyLists, copyLots bool, foods map[string]string) error {
	if source == target {
		return platform.Invalid("Không gian nguồn và đích phải khác nhau")
	}
	mapping, err := json.Marshal(foods)
	if err != nil {
		return err
	}
	if copyLists {
		rows, err := tx.Query(ctx, `SELECT id::text FROM shopping.shopping_lists WHERE space_id=$1 AND deleted_at IS NULL ORDER BY id FOR SHARE`, source)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, old := range ids {
			id := platform.ID()
			if _, err = tx.Exec(ctx, `INSERT INTO shopping.shopping_lists(id,space_id,name,planned_date,notes) SELECT $2,$3,name,planned_date,notes FROM shopping.shopping_lists WHERE id=$1`, old, id, target); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT id::text FROM shopping.shopping_items WHERE list_id=$1 AND space_id=$2 AND deleted_at IS NULL ORDER BY id FOR SHARE`, old, source)
			if err != nil {
				return err
			}
			items := []string{}
			for rows.Next() {
				var item string
				if err = rows.Scan(&item); err != nil {
					rows.Close()
					return err
				}
				items = append(items, item)
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			for _, item := range items {
				if _, err = tx.Exec(ctx, `INSERT INTO shopping.shopping_items(id,space_id,list_id,food_id,name,unit_id,unit_factor,dimension,quantity,notes) SELECT $2,$3,$4,COALESCE(($5::jsonb->>food_id::text)::uuid,food_id),name,unit_id,unit_factor,dimension,quantity,notes FROM shopping.shopping_items WHERE id=$1`, item, platform.ID(), target, id, mapping); err != nil {
					return err
				}
			}
		}
	}
	if copyLots {
		rows, err := tx.Query(ctx, `SELECT id::text FROM pantry.food_lots WHERE space_id=$1 AND lifecycle='active' AND deleted_at IS NULL ORDER BY id FOR SHARE`, source)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, old := range ids {
			id := platform.ID()
			if _, err = tx.Exec(ctx, `INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location,total_cost,notes) SELECT $2,$3,COALESCE(($4::jsonb->>food_id::text)::uuid,food_id),unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location,total_cost,notes FROM pantry.food_lots WHERE id=$1`, old, id, target, mapping); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,reason,actor_id,occurred_at) SELECT $2,space_id,id,food_id,'stock_in',base_remaining,'space_copy',$3,$4 FROM pantry.food_lots WHERE id=$1`, id, platform.ID(), user, s.db.Clock.Now()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) PurgeSpace(ctx context.Context, tx pgx.Tx, sid string) error {
	for _, query := range []string{`DELETE FROM pantry.stock_movements WHERE space_id=$1`, `DELETE FROM pantry.food_lots WHERE space_id=$1`, `DELETE FROM shopping.shopping_items WHERE space_id=$1`, `DELETE FROM shopping.shopping_lists WHERE space_id=$1`} {
		if _, err := tx.Exec(ctx, query, sid); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RemapFood(ctx context.Context, tx pgx.Tx, source, target string) error {
	if source == target {
		return nil
	}
	var name, sourceDimension, targetDimension string
	if err := tx.QueryRow(ctx, `SELECT t.name,su.dimension,tu.dimension FROM catalog.foods s JOIN catalog.units su ON su.id=s.default_unit_id JOIN catalog.foods t ON t.id=$2 JOIN catalog.units tu ON tu.id=t.default_unit_id WHERE s.id=$1 AND t.scope='global' AND t.status='active'`, source, target).Scan(&name, &sourceDimension, &targetDimension); err != nil {
		return err
	}
	if sourceDimension != targetDimension {
		return platform.Rule("Không thể gộp thực phẩm khác nhóm đại lượng")
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM shopping.shopping_items WHERE food_id IN ($1,$2) ORDER BY id FOR UPDATE`, source, target)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, query := range []string{`UPDATE pantry.food_lots SET food_id=$2,version=version+1 WHERE food_id=$1`, `UPDATE pantry.stock_movements SET food_id=$2 WHERE food_id=$1`, `UPDATE shopping.shopping_items SET food_id=$2,name=$3,version=version+1 WHERE food_id=$1`} {
		args := []any{source, target}
		if query == `UPDATE shopping.shopping_items SET food_id=$2,name=$3,version=version+1 WHERE food_id=$1` {
			args = append(args, name)
		}
		if _, err = tx.Exec(ctx, query, args...); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `SELECT space_id::text,list_id::text,dimension FROM shopping.shopping_items WHERE food_id=$1 AND status IN ('to_buy','assigned') AND deleted_at IS NULL GROUP BY space_id,list_id,dimension HAVING count(*)>1 ORDER BY space_id,list_id,dimension`, target)
	if err != nil {
		return err
	}
	type group struct{ space, list, dimension string }
	groups := []group{}
	for rows.Next() {
		var g group
		if err = rows.Scan(&g.space, &g.list, &g.dimension); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, g := range groups {
		var id, unit string
		var amount kernel.Quantity
		if err = tx.QueryRow(ctx, `SELECT min(id::text),sum(quantity*unit_factor)::text FROM shopping.shopping_items WHERE food_id=$1 AND space_id=$2 AND list_id=$3 AND dimension=$4 AND status IN ('to_buy','assigned') AND deleted_at IS NULL`, target, g.space, g.list, g.dimension).Scan(&id, &amount); err != nil {
			return err
		}
		if err = validQuantity(amount); err != nil {
			return platform.Rule("Đơn vị cơ sở không biểu diễn chính xác lượng gộp")
		}
		if err = tx.QueryRow(ctx, `SELECT id::text FROM catalog.units WHERE dimension=$1 AND is_base AND active`, g.dimension).Scan(&unit); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM shopping.shopping_items WHERE food_id=$1 AND space_id=$2 AND list_id=$3 AND dimension=$4 AND status IN ('to_buy','assigned') AND deleted_at IS NULL AND id<>$5`, target, g.space, g.list, g.dimension, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE shopping.shopping_items SET quantity=$2,unit_id=$3,unit_factor=1,status='to_buy',assignee_id=NULL,unit_price=NULL,version=version+1 WHERE id=$1`, id, amount.String(), unit); err != nil {
			return err
		}
	}
	return nil
}
