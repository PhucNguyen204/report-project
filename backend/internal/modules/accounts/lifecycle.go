package accounts

import (
	"context"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Service) ClonePrivateFoods(ctx context.Context, tx pgx.Tx, source, target string) (map[string]string, error) {
	if source == target {
		return nil, platform.Invalid("Không gian nguồn và đích phải khác nhau")
	}
	rows, err := tx.Query(ctx, `SELECT id::text,merged_into::text FROM catalog.foods WHERE scope='space' AND owner_space_id=$1 ORDER BY id FOR SHARE`, source)
	if err != nil {
		return nil, err
	}
	type privateFood struct {
		id     string
		merged *string
	}
	foods := []privateFood{}
	for rows.Next() {
		var food privateFood
		if err = rows.Scan(&food.id, &food.merged); err != nil {
			rows.Close()
			return nil, err
		}
		foods = append(foods, food)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, food := range foods {
		id := food.id
		if food.merged != nil {
			result[id] = *food.merged
			continue
		}
		clone := platform.ID()
		if err = tx.QueryRow(ctx, `INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id,status,default_location,merged_into) SELECT $2,name,category_id,'space',$3,default_unit_id,status,default_location,merged_into FROM catalog.foods WHERE id=$1 ON CONFLICT(owner_space_id,category_id,lower(name)) WHERE scope='space' DO UPDATE SET name=catalog.foods.name RETURNING id::text`, id, clone, target).Scan(&clone); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO catalog.food_shelf_lives(food_id,storage_location,days) SELECT $2,storage_location,days FROM catalog.food_shelf_lives WHERE food_id=$1 ON CONFLICT(food_id,storage_location) DO NOTHING`, id, clone); err != nil {
			return nil, err
		}
		result[id] = clone
	}
	return result, nil
}

func (s *Service) PurgeSpace(ctx context.Context, tx pgx.Tx, sid string) error {
	var dissolved bool
	err := tx.QueryRow(ctx, `SELECT dissolved_at IS NOT NULL FROM household.spaces WHERE id=$1 FOR UPDATE`, sid).Scan(&dissolved)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !dissolved {
		return platform.Rule("Chỉ xóa không gian đã giải tán")
	}
	for _, query := range []string{
		`DELETE FROM catalog.food_shelf_lives WHERE food_id IN (SELECT id FROM catalog.foods WHERE owner_space_id=$1)`,
		`DELETE FROM catalog.foods WHERE owner_space_id=$1`,
		`DELETE FROM household.invite_codes WHERE space_id=$1`,
		`DELETE FROM household.space_members WHERE space_id=$1`,
		`UPDATE identity.users SET last_space_id=NULL WHERE last_space_id=$1`,
		`DELETE FROM household.spaces WHERE id=$1`,
	} {
		if _, err = tx.Exec(ctx, query, sid); err != nil {
			return err
		}
	}
	return nil
}
