package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

const globalScope = "00000000-0000-7000-8000-000000000000"

type TemplateInput struct {
	RecipeInput
	Status string `json:"status"`
}

func (s *Service) requireAdmin(ctx context.Context, tx pgx.Tx, user string) error {
	role, err := s.db.LockActor(ctx, tx, user)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "admin") {
		return platform.Forbidden()
	}
	return err
}
func (s *Service) readTemplate(ctx context.Context, q platform.Querier, id string) (Recipe, error) {
	var out Recipe
	var b []byte
	if err := q.QueryRow(ctx, "SELECT id::text,name,servings,kind,status,meal_slot,cook_minutes,steps,notes,image_object_id::text,version,created_by::text FROM recipe.recipes WHERE id=$1 AND kind='template' AND deleted_at IS NULL", id).Scan(&out.ID, &out.Name, &out.Servings, &out.Kind, &out.Status, &out.MealSlot, &out.CookMinutes, &b, &out.Notes, &out.ImageObjectID, &out.Version, &out.CreatedBy); err != nil {
		return out, err
	}
	if err := json.Unmarshal(b, &out.Steps); err != nil {
		return out, err
	}
	rows, err := q.Query(ctx, "SELECT food_id::text,unit_id::text,quantity::text,optional FROM recipe.recipe_ingredients WHERE recipe_id=$1 ORDER BY food_id", id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Ingredients = []IngredientInput{}
	for rows.Next() {
		var line IngredientInput
		var amount string
		if err = rows.Scan(&line.FoodID, &line.UnitID, &amount, &line.Optional); err != nil {
			return out, err
		}
		line.Quantity, err = kernel.ParseQuantity(amount)
		if err != nil {
			return out, err
		}
		out.Ingredients = append(out.Ingredients, line)
	}
	return out, rows.Err()
}
func (s *Service) SaveTemplate(ctx context.Context, user, id string, version int64, in TemplateInput) (Recipe, error) {
	var out Recipe
	if err := validateRecipe(in.RecipeInput); err != nil {
		return out, err
	}
	if in.Status == "" {
		in.Status = "draft"
	}
	if in.Status != "draft" && in.Status != "published" && in.Status != "hidden" {
		return out, platform.Invalid("Trạng thái công thức mẫu không hợp lệ")
	}
	creating := id == ""
	if creating {
		id = in.ID
		if id == "" {
			id = platform.ID()
		}
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if err := s.requireAdmin(ctx, tx, user); err != nil {
			return err
		}
		var old any
		if creating {
			_, err := tx.Exec(ctx, "INSERT INTO recipe.recipes(id,created_by,name,servings,kind,status) VALUES($1,$2,$3,$4,'template',$5)", id, user, in.Name, in.Servings, in.Status)
			if err != nil {
				return err
			}
		} else {
			var current int64
			if err := tx.QueryRow(ctx, "SELECT version FROM recipe.recipes WHERE id=$1 AND kind='template' AND deleted_at IS NULL FOR UPDATE", id).Scan(&current); err != nil {
				return err
			}
			if version <= 0 || version != current {
				return platform.Conflict()
			}
			previous, err := s.readTemplate(ctx, tx, id)
			if err != nil {
				return err
			}
			old = previous
			if _, err = tx.Exec(ctx, "DELETE FROM recipe.recipe_ingredients WHERE recipe_id=$1", id); err != nil {
				return err
			}
		}
		steps, _ := json.Marshal(in.Steps)
		if in.Steps == nil {
			steps = []byte("[]")
		}
		if _, err := tx.Exec(ctx, "UPDATE recipe.recipes SET name=$2,servings=$3,status=$4,meal_slot=$5,cook_minutes=$6,steps=$7,notes=$8,version=CASE WHEN $9 THEN version ELSE version+1 END WHERE id=$1", id, in.Name, in.Servings, in.Status, in.MealSlot, in.CookMinutes, steps, in.Notes, version == 0); err != nil {
			return err
		}
		for _, line := range in.Ingredients {
			var global bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM catalog.foods WHERE id=$1 AND scope='global' AND status='active')", line.FoodID).Scan(&global); err != nil {
				return err
			}
			if !global {
				return platform.NotFound()
			}
		}
		if err := s.writeIngredients(ctx, tx, globalScope, id, in.Ingredients); err != nil {
			return err
		}
		if err := s.writeRecipeImage(ctx, tx, user, id, in.ImageObjectID); err != nil {
			return err
		}
		var err error
		out, err = s.readTemplate(ctx, tx, id)
		if err != nil {
			return err
		}
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(out)
		_, err = tx.Exec(ctx, "INSERT INTO admin.audit_logs(id,actor_id,action,entity_id,old_value,new_value) VALUES($1,$2,'recipe_template.save',$3,$4,$5)", platform.ID(), user, id, before, after)
		return err
	})
	return out, err
}
func (s *Service) DeleteTemplate(ctx context.Context, user, id string) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		if err := s.requireAdmin(ctx, tx, user); err != nil {
			return err
		}
		var current int64
		if err := tx.QueryRow(ctx, "SELECT version FROM recipe.recipes WHERE id=$1 AND kind='template' AND deleted_at IS NULL FOR UPDATE", id).Scan(&current); err != nil {
			return err
		}
		var used bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM mealplan.meal_entries WHERE recipe_id=$1 AND deleted_at IS NULL) OR EXISTS(SELECT 1 FROM recipe.recipes WHERE origin_recipe_id=$1 AND deleted_at IS NULL)", id).Scan(&used); err != nil {
			return err
		}
		if used {
			return platform.Rule("Công thức đang được tham chiếu; hãy chuyển sang ẩn")
		}
		if _, err := tx.Exec(ctx, "UPDATE recipe.recipes SET deleted_at=$2,status='hidden',version=version+1 WHERE id=$1", id, s.db.Clock.Now()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO admin.audit_logs(id,actor_id,action,entity_id) VALUES($1,$2,'recipe_template.delete',$3)", platform.ID(), user, id)
		return err
	})
}
