package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"math"
	"math/big"
	"time"
)

type Service struct {
	ValidateMedia func(context.Context, pgx.Tx, string, string, string, string) error
	db            *platform.DB
	stock         platform.StockPort
	lists         platform.ListComposer
	catalog       platform.CatalogPort
}

func New(db *platform.DB, stock platform.StockPort, lists platform.ListComposer, catalog platform.CatalogPort) *Service {
	return &Service{db: db, stock: stock, lists: lists, catalog: catalog}
}

type Recipe struct {
	ImageObjectID *string           `json:"imageObjectId,omitempty"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Servings      int               `json:"servings"`
	Kind          string            `json:"kind"`
	Status        string            `json:"status"`
	MealSlot      string            `json:"mealSlot"`
	CookMinutes   int               `json:"cookMinutes"`
	Steps         []string          `json:"steps"`
	Notes         string            `json:"notes"`
	Version       int64             `json:"version"`
	Ingredients   []IngredientInput `json:"ingredients"`
	Comparison    []Comparison      `json:"comparison,omitempty"`
	CreatedBy     string            `json:"-"`
}
type Comparison struct {
	FoodID    string          `json:"foodId"`
	UnitID    string          `json:"unitId"`
	Required  kernel.Quantity `json:"required"`
	Available kernel.Quantity `json:"available"`
	ShortQty  kernel.Quantity `json:"shortQty"`
	Optional  bool            `json:"optional"`
	Dimension string          `json:"dimension"`
}

func scaled(q kernel.Quantity, numerator, denominator int) (kernel.Quantity, error) {
	if denominator <= 0 || numerator <= 0 {
		return 0, platform.Invalid("Khẩu phần không hợp lệ")
	}
	n := new(big.Int).Mul(big.NewInt(int64(q)), big.NewInt(int64(numerator)))
	rem := new(big.Int)
	n.QuoRem(n, big.NewInt(int64(denominator)), rem)
	if !n.IsInt64() || rem.Sign() != 0 {
		return 0, platform.Invalid("Không biểu diễn chính xác lượng nguyên liệu")
	}
	return kernel.Quantity(n.Int64()), nil
}

func (s *Service) writeIngredients(ctx context.Context, tx pgx.Tx, sid, rid string, lines []IngredientInput) error {
	for _, line := range lines {
		unit, err := s.catalog.ValidateFood(ctx, tx, sid, line.FoodID, line.UnitID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO recipe.recipe_ingredients(id,recipe_id,food_id,unit_id,unit_factor,dimension,quantity,optional) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", platform.ID(), rid, unit.FoodID, unit.UnitID, unit.Factor.String(), unit.Dimension, line.Quantity.String(), line.Optional); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) CreateRecipe(ctx context.Context, user, sid string, in RecipeInput) (Recipe, error) {
	var out Recipe
	if err := validateRecipe(in); err != nil {
		return out, err
	}
	rid := in.ID
	if rid == "" {
		rid = platform.ID()
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		steps, _ := json.Marshal(in.Steps)
		if in.Steps == nil {
			steps = []byte("[]")
		}
		if _, err := tx.Exec(ctx, "INSERT INTO recipe.recipes(id,space_id,created_by,name,servings,meal_slot,cook_minutes,steps,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", rid, sid, user, in.Name, in.Servings, in.MealSlot, in.CookMinutes, steps, in.Notes); err != nil {
			return err
		}
		if err := s.writeIngredients(ctx, tx, sid, rid, in.Ingredients); err != nil {
			return err
		}
		if err := s.writeRecipeImage(ctx, tx, user, rid, in.ImageObjectID); err != nil {
			return err
		}
		return s.db.Activity(ctx, tx, sid, user, "recipe.created", rid, in)
	})
	if err != nil {
		return out, err
	}
	return s.GetRecipe(ctx, user, sid, rid, in.Servings)
}

func (s *Service) readRecipe(ctx context.Context, q platform.Querier, sid, rid string, servings int) (Recipe, error) {
	var out Recipe
	var steps []byte
	err := q.QueryRow(ctx, "SELECT id::text,name,servings,kind,status,meal_slot,cook_minutes,steps,notes,image_object_id::text,version,created_by::text FROM recipe.recipes WHERE id=$1 AND deleted_at IS NULL AND status='published' AND (space_id=$2 OR kind='template')", rid, sid).Scan(&out.ID, &out.Name, &out.Servings, &out.Kind, &out.Status, &out.MealSlot, &out.CookMinutes, &steps, &out.Notes, &out.ImageObjectID, &out.Version, &out.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, platform.NotFound()
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(steps, &out.Steps); err != nil {
		return out, err
	}
	if servings == 0 {
		servings = out.Servings
	}
	if servings < 1 || servings > 20 {
		return out, platform.Invalid("Khẩu phần từ 1–20")
	}
	rows, err := q.Query(ctx, "SELECT food_id::text,unit_id::text,quantity::text,optional FROM recipe.recipe_ingredients WHERE recipe_id=$1 ORDER BY food_id", rid)
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
		raw, e := kernel.ParseQuantity(amount)
		if e != nil {
			return out, e
		}
		line.Quantity, e = scaled(raw, servings, out.Servings)
		if e != nil {
			return out, e
		}
		out.Ingredients = append(out.Ingredients, line)
	}
	return out, rows.Err()
}
func (s *Service) GetRecipe(ctx context.Context, user, sid, rid string, servings int) (Recipe, error) {
	var out Recipe
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(ctx, tx, sid, user); err != nil {
			return err
		}
		var err error
		out, err = s.readRecipe(ctx, tx, sid, rid, servings)
		return err
	})
	return out, err
}
func (s *Service) UpdateRecipe(ctx context.Context, user, sid, rid string, version int64, in RecipeInput) (Recipe, error) {
	var out Recipe
	if err := validateRecipe(in); err != nil {
		return out, err
	}
	if version <= 0 {
		return out, platform.Conflict()
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		role, err := s.db.LockMember(ctx, tx, sid, user)
		if err != nil {
			return err
		}
		var owner, kind string
		var current int64
		if err = tx.QueryRow(ctx, "SELECT created_by::text,kind,version FROM recipe.recipes WHERE id=$1 AND (space_id=$2 OR (kind='template' AND status='published')) AND deleted_at IS NULL FOR UPDATE", rid, sid).Scan(&owner, &kind, &current); errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		} else if err != nil {
			return err
		}
		if kind != "space" || (owner != user && role != "leader") {
			return platform.Forbidden()
		}
		if current != version {
			return platform.Conflict()
		}
		steps, _ := json.Marshal(in.Steps)
		if in.Steps == nil {
			steps = []byte("[]")
		}
		if _, err = tx.Exec(ctx, "UPDATE recipe.recipes SET name=$3,servings=$4,meal_slot=$5,cook_minutes=$6,steps=$7,notes=$8,version=version+1 WHERE id=$1 AND space_id=$2", rid, sid, in.Name, in.Servings, in.MealSlot, in.CookMinutes, steps, in.Notes); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM recipe.recipe_ingredients WHERE recipe_id=$1", rid); err != nil {
			return err
		}
		if err = s.writeIngredients(ctx, tx, sid, rid, in.Ingredients); err != nil {
			return err
		}
		if err := s.writeRecipeImage(ctx, tx, user, rid, in.ImageObjectID); err != nil {
			return err
		}
		return s.db.Activity(ctx, tx, sid, user, "recipe.updated", rid, in)
	})
	if err != nil {
		return out, err
	}
	return s.GetRecipe(ctx, user, sid, rid, in.Servings)
}
func (s *Service) DeleteRecipe(ctx context.Context, user, sid, rid string) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		role, err := s.db.LockMember(ctx, tx, sid, user)
		if err != nil {
			return err
		}
		var owner, kind string
		if err = tx.QueryRow(ctx, "SELECT created_by::text,kind FROM recipe.recipes WHERE id=$1 AND (space_id=$2 OR (kind='template' AND status='published')) AND deleted_at IS NULL FOR UPDATE", rid, sid).Scan(&owner, &kind); err != nil {
			return err
		}
		if kind != "space" || (owner != user && role != "leader") {
			return platform.Forbidden()
		}
		if _, err = tx.Exec(ctx, "UPDATE recipe.recipes SET deleted_at=$3,version=version+1 WHERE id=$1 AND space_id=$2", rid, sid, s.db.Clock.Now()); err != nil {
			return err
		}
		if err = s.OnRecipeDelete(ctx, tx, sid, rid); err != nil {
			return err
		}
		return s.db.Activity(ctx, tx, sid, user, "recipe.deleted", rid, nil)
	})
}
func (s *Service) OnRecipeDelete(ctx context.Context, tx pgx.Tx, sid, rid string) error {
	_, err := tx.Exec(ctx, "UPDATE mealplan.meal_entries SET recipe_id=NULL,version=version+1 WHERE space_id=$1 AND recipe_id=$2 AND status='planned' AND deleted_at IS NULL", sid, rid)
	return err
}
func (s *Service) SetFavorite(ctx context.Context, user, rid string, on bool) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		role, err := s.db.LockActor(ctx, tx, user)
		if err != nil {
			return err
		}
		var kind string
		var sid *string
		err = tx.QueryRow(ctx, "SELECT kind,space_id::text FROM recipe.recipes WHERE id=$1 AND status='published' AND deleted_at IS NULL", rid).Scan(&kind, &sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		if kind == "space" {
			if role != "user" || sid == nil {
				return platform.NotFound()
			}
			if _, err := s.db.LockMember(ctx, tx, *sid, user); err != nil {
				return err
			}
		} else if kind != "template" {
			return platform.NotFound()
		}
		var visible bool
		err = tx.QueryRow(ctx, "SELECT true FROM recipe.recipes WHERE id=$1 AND kind=$2 AND space_id IS NOT DISTINCT FROM $3::uuid AND status='published' AND deleted_at IS NULL FOR SHARE", rid, kind, sid).Scan(&visible)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		if on {
			_, err := tx.Exec(ctx, "INSERT INTO recipe.favorite_recipes(user_id,recipe_id) VALUES($1,$2) ON CONFLICT DO NOTHING", user, rid)
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM recipe.favorite_recipes WHERE user_id=$1 AND recipe_id=$2", user, rid)
		return err
	})
}
func (s *Service) CopyRecipe(ctx context.Context, user, sid, rid, name string) (Recipe, error) {
	var out Recipe
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		recipe, err := s.readRecipe(ctx, tx, sid, rid, 0)
		if err != nil {
			return err
		}
		if recipe.Kind != "template" {
			return platform.NotFound()
		}
		if name == "" {
			name = recipe.Name
		}
		in := RecipeInput{Name: name, Servings: recipe.Servings, MealSlot: recipe.MealSlot, CookMinutes: recipe.CookMinutes, Steps: recipe.Steps, Notes: recipe.Notes, Ingredients: recipe.Ingredients}
		if err = validateRecipe(in); err != nil {
			return err
		}
		id := platform.ID()
		steps, _ := json.Marshal(in.Steps)
		if _, err = tx.Exec(ctx, "INSERT INTO recipe.recipes(id,space_id,created_by,name,servings,meal_slot,cook_minutes,steps,notes,origin_recipe_id,image_object_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", id, sid, user, in.Name, in.Servings, in.MealSlot, in.CookMinutes, steps, in.Notes, rid, recipe.ImageObjectID); err != nil {
			return err
		}
		if err = s.writeIngredients(ctx, tx, sid, id, in.Ingredients); err != nil {
			return err
		}
		out, err = s.readRecipe(ctx, tx, sid, id, 0)
		if err != nil {
			return err
		}
		return s.db.Activity(ctx, tx, sid, user, "recipe.copied", id, map[string]string{"originRecipeId": rid})
	})
	return out, err
}

// Ingredients returns serving-scaled quantities normalized to base units.
type normalizedIngredient struct {
	platform.Ingredient
	DisplayUnit string
}

func (s *Service) Ingredients(ctx context.Context, tx pgx.Tx, sid, rid string, servings kernel.Quantity) ([]platform.Ingredient, error) {
	needs, err := s.normalizedIngredients(ctx, tx, sid, rid, servings)
	if err != nil {
		return nil, err
	}
	out := make([]platform.Ingredient, 0, len(needs))
	for _, need := range needs {
		out = append(out, need.Ingredient)
	}
	return out, nil
}

func (s *Service) normalizedIngredients(ctx context.Context, tx pgx.Tx, sid, rid string, servings kernel.Quantity) ([]normalizedIngredient, error) {
	if servings < kernel.Quantity(kernel.Scale) || servings > kernel.Quantity(20*kernel.Scale) || int64(servings)%kernel.Scale != 0 {
		return nil, platform.Invalid("Khẩu phần phải là số nguyên từ 1–20")
	}
	r, err := s.readRecipe(ctx, tx, sid, rid, 0)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT i.food_id::text,COALESCE(f.merged_into,f.id)::text,i.unit_factor::text,i.dimension,b.id::text FROM recipe.recipe_ingredients i JOIN catalog.foods f ON f.id=i.food_id JOIN catalog.units b ON b.dimension=i.dimension AND b.is_base WHERE i.recipe_id=$1`, rid)
	if err != nil {
		return nil, err
	}
	type snapshot struct {
		food, dimension, base string
		factor                kernel.Quantity
	}
	factors := map[string]snapshot{}
	for rows.Next() {
		var original, food, factor, dimension, base string
		if err = rows.Scan(&original, &food, &factor, &dimension, &base); err != nil {
			rows.Close()
			return nil, err
		}
		f, e := kernel.ParseQuantity(factor)
		if e != nil {
			rows.Close()
			return nil, e
		}
		factors[original] = snapshot{food, dimension, base, f}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := []normalizedIngredient{}
	for _, line := range r.Ingredients {
		u, ok := factors[line.FoodID]
		if !ok {
			return nil, platform.NotFound()
		}
		q, err := line.Quantity.Mul(u.factor)
		if err != nil {
			return nil, platform.Invalid("Không biểu diễn chính xác lượng nguyên liệu")
		}
		q, err = scaled(q, int(int64(servings)/kernel.Scale), r.Servings)
		if err != nil {
			return nil, err
		}
		out = append(out, normalizedIngredient{Ingredient: platform.Ingredient{FoodID: u.food, UnitID: u.base, Dimension: u.dimension, Quantity: q, Optional: line.Optional}, DisplayUnit: line.UnitID})
	}
	return out, nil
}

func parseDate(v string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return d, platform.Invalid("Ngày không hợp lệ")
	}
	return d, nil
}

func (s *Service) Compare(ctx context.Context, user, sid, rid string, servings int) ([]Comparison, error) {
	out := []Comparison{}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(ctx, tx, sid, user); err != nil {
			return err
		}
		needs, err := s.Ingredients(ctx, tx, sid, rid, platform.Quantity(int64(servings)*1_000_000))
		if err != nil {
			return err
		}
		if s.stock == nil {
			return platform.Err(503, 5003, "Dịch vụ tồn kho chưa sẵn sàng")
		}
		stocks, err := s.stock.Available(ctx, tx, sid, s.db.Today())
		if err != nil {
			return err
		}
		for _, need := range needs {
			line := Comparison{FoodID: need.FoodID, UnitID: need.UnitID, Dimension: need.Dimension, Required: need.Quantity, Optional: need.Optional}
			for _, stock := range stocks {
				if stock.FoodID == need.FoodID && stock.Dimension == need.Dimension {
					if stock.Quantity < 0 || int64(line.Available) > math.MaxInt64-int64(stock.Quantity) {
						return platform.Invalid("Lượng tồn kho vượt giới hạn")
					}
					line.Available += stock.Quantity
				}
			}
			line.ShortQty = max(0, line.Required-line.Available)
			out = append(out, line)
		}
		return nil
	})
	return out, err
}

func (s *Service) writeRecipeImage(ctx context.Context, tx pgx.Tx, user, rid string, image *string) error {
	var id string
	if image != nil {
		id = *image
	}
	if id != "" {
		if s.ValidateMedia == nil {
			return platform.Rule("Dịch vụ xác thực ảnh chưa được cấu hình")
		}
		if err := s.ValidateMedia(ctx, tx, user, "recipe", rid, id); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, "UPDATE recipe.recipes SET image_object_id=NULLIF($2,'')::uuid WHERE id=$1", rid, id)
	return err
}
