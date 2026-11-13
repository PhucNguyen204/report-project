package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

// CopySpace copies active recipes after the caller has cloned private foods.
func (s *Service) CopySpace(ctx context.Context, tx pgx.Tx, source, target, user string, foodMap map[string]string) error {
	if source == target {
		return platform.Invalid("Không thể sao chép vào cùng không gian")
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "recipe.copy:"+target); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT id::text FROM recipe.recipes WHERE space_id=$1 AND created_by=$2 AND deleted_at IS NULL ORDER BY id FOR SHARE", source, user)
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
	for _, sourceID := range ids {
		var copied bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM recipe.recipes WHERE space_id=$1 AND origin_recipe_id=$2)", target, sourceID).Scan(&copied); err != nil {
			return err
		}
		if copied {
			continue
		}
		id := platform.ID()
		if _, err = tx.Exec(ctx, `INSERT INTO recipe.recipes(id,space_id,created_by,name,servings,kind,status,meal_slot,cook_minutes,steps,notes,origin_recipe_id,image_object_id) SELECT $1,$2,$3,name,servings,'space','published',meal_slot,cook_minutes,steps,notes,id,image_object_id FROM recipe.recipes WHERE id=$4`, id, target, user, sourceID); err != nil {
			return err
		}
		ingredients, err := tx.Query(ctx, "SELECT food_id::text,unit_id::text,unit_factor::text,dimension,quantity::text,optional FROM recipe.recipe_ingredients WHERE recipe_id=$1 ORDER BY food_id", sourceID)
		if err != nil {
			return err
		}
		type line struct {
			food, unit, factor, dimension, quantity string
			optional                                bool
		}
		lines := []line{}
		for ingredients.Next() {
			var v line
			if err = ingredients.Scan(&v.food, &v.unit, &v.factor, &v.dimension, &v.quantity, &v.optional); err != nil {
				ingredients.Close()
				return err
			}
			lines = append(lines, v)
		}
		ingredients.Close()
		if err = ingredients.Err(); err != nil {
			return err
		}
		for _, v := range lines {
			if mapped, ok := foodMap[v.food]; ok {
				v.food = mapped
			}
			u, err := s.catalog.ValidateFood(ctx, tx, target, v.food, v.unit)
			if err != nil {
				return err
			}
			v.food = u.FoodID
			if _, err = tx.Exec(ctx, "INSERT INTO recipe.recipe_ingredients(id,recipe_id,food_id,unit_id,unit_factor,dimension,quantity,optional) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", platform.ID(), id, v.food, v.unit, v.factor, v.dimension, v.quantity, v.optional); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) PurgeSpace(ctx context.Context, tx pgx.Tx, sid string) error {
	for _, sql := range []string{
		"DELETE FROM mealplan.meal_entries WHERE space_id=$1",
		"DELETE FROM recipe.favorite_recipes WHERE recipe_id IN (SELECT id FROM recipe.recipes WHERE space_id=$1)",
		"DELETE FROM recipe.recipe_ingredients WHERE recipe_id IN (SELECT id FROM recipe.recipes WHERE space_id=$1)",
		"DELETE FROM recipe.recipes WHERE space_id=$1",
	} {
		if _, err := tx.Exec(ctx, sql, sid); err != nil {
			return err
		}
	}
	return nil
}

// RemapFood keeps ingredient totals exact when a private food is merged.
func (s *Service) RemapFood(ctx context.Context, tx pgx.Tx, source, target string) error {
	if source == target {
		return nil
	}
	var dimension, baseUnit string
	if err := tx.QueryRow(ctx, `SELECT d.dimension,b.id::text FROM catalog.foods f JOIN catalog.units d ON d.id=f.default_unit_id JOIN catalog.units b ON b.dimension=d.dimension AND b.is_base WHERE f.id=$1`, target).Scan(&dimension, &baseUnit); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT id::text FROM recipe.recipes WHERE id IN (SELECT recipe_id FROM recipe.recipe_ingredients WHERE food_id=$1) ORDER BY id FOR UPDATE", source)
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
	for _, rid := range ids {
		rows, err := tx.Query(ctx, "SELECT id::text,dimension,quantity::text,unit_factor::text,optional FROM recipe.recipe_ingredients WHERE recipe_id=$1 AND food_id IN ($2,$3) ORDER BY id FOR UPDATE", rid, source, target)
		if err != nil {
			return err
		}
		var total kernel.Quantity
		optional := true
		count := 0
		for rows.Next() {
			var id, dim, amount, factor string
			var opt bool
			if err = rows.Scan(&id, &dim, &amount, &factor, &opt); err != nil {
				rows.Close()
				return err
			}
			if dim != dimension {
				rows.Close()
				return platform.Rule("Không thể gộp khác nhóm đại lượng")
			}
			q, e := kernel.ParseQuantity(amount)
			if e != nil {
				rows.Close()
				return e
			}
			f, e := kernel.ParseQuantity(factor)
			if e != nil {
				rows.Close()
				return e
			}
			q, e = q.Mul(f)
			if e != nil {
				rows.Close()
				return e
			}
			sum, e := safeCost(int64(total), int64(q))
			if e != nil {
				rows.Close()
				return e
			}
			total = kernel.Quantity(sum)
			optional = optional && opt
			count++
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if count == 0 {
			continue
		}
		if !total.TwoDecimals() {
			return platform.Invalid("Không biểu diễn chính xác lượng sau gộp")
		}
		if _, err = tx.Exec(ctx, "DELETE FROM recipe.recipe_ingredients WHERE recipe_id=$1 AND food_id IN ($2,$3)", rid, source, target); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO recipe.recipe_ingredients(id,recipe_id,food_id,unit_id,unit_factor,dimension,quantity,optional) VALUES($1,$2,$3,$4,1,$5,$6,$7)", platform.ID(), rid, target, baseUnit, dimension, total.String(), optional); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE recipe.recipes SET version=version+1 WHERE id=$1", rid); err != nil {
			return err
		}
	}
	return nil
}
