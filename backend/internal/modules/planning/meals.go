package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
	"unicode/utf8"
)

type MealInput struct {
	ID       string `json:"id,omitempty"`
	RecipeID string `json:"recipeId,omitempty"`
	Name     string `json:"name,omitempty"`
	Date     string `json:"date"`
	MealSlot string `json:"mealSlot"`
	Servings int    `json:"servings"`
}
type Meal struct {
	ID       string  `json:"id"`
	RecipeID *string `json:"recipeId"`
	Name     string  `json:"name"`
	Date     string  `json:"date"`
	MealSlot string  `json:"mealSlot"`
	Servings int     `json:"servings"`
	Status   string  `json:"status"`
	Version  int64   `json:"version"`
}
type CookLine struct {
	FoodID       string          `json:"foodId"`
	UnitID       string          `json:"unitId"`
	Quantity     kernel.Quantity `json:"quantity"`
	LotID        string          `json:"lotId,omitempty"`
	AllowExpired bool            `json:"allowExpired,omitempty"`
}
type CookInput struct {
	Version   int64      `json:"-"`
	Servings  int        `json:"servings,omitempty"`
	Lines     []CookLine `json:"lines,omitempty"`
	SkipStock bool       `json:"skipStock"`
}

func (s *Service) horizon(ctx context.Context, q platform.Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, "SELECT (value #>> '{}')::int FROM admin.system_configs WHERE key='mealplan.horizonDays'").Scan(&n)
	if err != nil {
		return 0, err
	}
	if n < 7 || n > 60 {
		return 0, platform.Invalid("Khoảng lập kế hoạch không hợp lệ")
	}
	return n, nil
}
func (s *Service) prepareMeal(ctx context.Context, tx pgx.Tx, sid string, in MealInput) (MealInput, time.Time, error) {
	on, err := parseDate(in.Date)
	if err != nil {
		return in, on, err
	}
	horizon, err := s.horizon(ctx, tx)
	if err != nil {
		return in, on, err
	}
	if err = validateMealDate(s.db.Today(), on, horizon); err != nil {
		return in, on, err
	}
	if in.Servings < 1 || in.Servings > 20 {
		return in, on, platform.Invalid("Khẩu phần từ 1–20")
	}
	if in.MealSlot != "breakfast" && in.MealSlot != "lunch" && in.MealSlot != "dinner" {
		return in, on, platform.Invalid("Bữa ăn không hợp lệ")
	}
	if in.RecipeID != "" {
		r, err := s.readRecipe(ctx, tx, sid, in.RecipeID, 0)
		if err != nil {
			return in, on, err
		}
		in.Name = r.Name
	} else if n := utf8.RuneCountInString(in.Name); n < 2 || n > 100 {
		return in, on, platform.Invalid("Tên món dài 2–100 ký tự")
	}
	return in, on, nil
}
func (s *Service) CreateMeal(ctx context.Context, user, sid string, in MealInput) (Meal, error) {
	var out Meal
	id := in.ID
	if id == "" {
		id = platform.ID()
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		in, on, err := s.prepareMeal(ctx, tx, sid, in)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO mealplan.meal_entries(id,space_id,recipe_id,name,meal_date,meal_slot,servings,created_by) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8)", id, sid, in.RecipeID, in.Name, on, in.MealSlot, in.Servings, user); err != nil {
			return err
		}
		out = Meal{ID: id, Name: in.Name, Date: in.Date, MealSlot: in.MealSlot, Servings: in.Servings, Status: "planned", Version: 1}
		if in.RecipeID != "" {
			out.RecipeID = &in.RecipeID
		}
		return s.db.Activity(ctx, tx, sid, user, "meal.created", id, in)
	})
	return out, err
}
func readMeal(ctx context.Context, q platform.Querier, sid, id string, lock bool) (Meal, error) {
	var m Meal
	sql := "SELECT id::text,recipe_id::text,name,meal_date::text,meal_slot,servings,status,version FROM mealplan.meal_entries WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL"
	if lock {
		sql += " FOR UPDATE"
	}
	err := q.QueryRow(ctx, sql, id, sid).Scan(&m.ID, &m.RecipeID, &m.Name, &m.Date, &m.MealSlot, &m.Servings, &m.Status, &m.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, platform.NotFound()
	}
	return m, err
}
func (s *Service) UpdateMeal(ctx context.Context, user, sid, id string, version int64, in MealInput) (Meal, error) {
	var out Meal
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		old, err := readMeal(ctx, tx, sid, id, true)
		if err != nil {
			return err
		}
		if old.Status == "cooked" {
			return platform.Rule("Món đã nấu không thể sửa")
		}
		if version <= 0 || old.Version != version {
			return platform.Conflict()
		}
		in, on, err := s.prepareMeal(ctx, tx, sid, in)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE mealplan.meal_entries SET recipe_id=NULLIF($3,'')::uuid,name=$4,meal_date=$5,meal_slot=$6,servings=$7,version=version+1 WHERE id=$1 AND space_id=$2", id, sid, in.RecipeID, in.Name, on, in.MealSlot, in.Servings); err != nil {
			return err
		}
		out = Meal{ID: id, Name: in.Name, Date: in.Date, MealSlot: in.MealSlot, Servings: in.Servings, Status: "planned", Version: old.Version + 1}
		if in.RecipeID != "" {
			out.RecipeID = &in.RecipeID
		}
		return s.db.Activity(ctx, tx, sid, user, "meal.updated", id, in)
	})
	return out, err
}
func (s *Service) DeleteMeal(ctx context.Context, user, sid, id string) error {
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		m, err := readMeal(ctx, tx, sid, id, true)
		if err != nil {
			return err
		}
		if m.Status == "cooked" {
			return platform.Rule("Món đã nấu không thể xóa")
		}
		if _, err = tx.Exec(ctx, "UPDATE mealplan.meal_entries SET deleted_at=$3,version=version+1 WHERE id=$1 AND space_id=$2", id, sid, s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Activity(ctx, tx, sid, user, "meal.deleted", id, nil)
	})
}

// Cook commits meal state and all stock movements in one transaction.
func (s *Service) Cook(ctx context.Context, user, sid, id string, in CookInput) (Meal, error) {
	var out Meal
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		m, err := readMeal(ctx, tx, sid, id, true)
		if err != nil {
			return err
		}
		out = m
		if m.Status == "cooked" {
			return nil
		}
		if in.Version != 0 && in.Version != m.Version {
			return platform.Conflict()
		}
		on, err := parseDate(m.Date)
		if err != nil {
			return err
		}
		if on.After(dateUTC(s.db.Today())) {
			return platform.Rule("Không xác nhận nấu món trong tương lai")
		}
		if m.RecipeID == nil {
			return platform.Rule("Món không có công thức")
		}
		servings := in.Servings
		if servings == 0 {
			servings = m.Servings
		}
		if servings < 1 || servings > 20 {
			return platform.Invalid("Khẩu phần từ 1–20")
		}
		if !in.SkipStock {
			ingredients, err := s.Ingredients(ctx, tx, sid, *m.RecipeID, kernel.Quantity(int64(servings)*kernel.Scale))
			if err != nil {
				return err
			}
			lines := []platform.Consumption{}
			allowed := map[string]bool{}
			for _, ingredient := range ingredients {
				allowed[ingredient.FoodID] = true
				if !ingredient.Optional {
					lines = append(lines, platform.Consumption{FoodID: ingredient.FoodID, Quantity: ingredient.Quantity})
				}
			}
			if in.Lines != nil {
				lines = []platform.Consumption{}
				seen := map[string]bool{}
				for _, line := range in.Lines {
					u, err := s.catalog.ValidateFood(ctx, tx, sid, line.FoodID, line.UnitID)
					if err != nil {
						return err
					}
					if !allowed[u.FoodID] || seen[u.FoodID] || line.Quantity <= 0 || !line.Quantity.TwoDecimals() {
						return platform.Invalid("Dòng trừ kho không hợp lệ")
					}
					seen[u.FoodID] = true
					quantity, err := line.Quantity.Mul(u.Factor)
					if err != nil {
						return err
					}
					lines = append(lines, platform.Consumption{FoodID: u.FoodID, LotID: line.LotID, Quantity: quantity, AllowExpired: line.AllowExpired})
				}
			}
			if s.stock == nil {
				return platform.Err(503, 5003, "Dịch vụ tồn kho chưa sẵn sàng")
			}
			if err = s.stock.Consume(ctx, tx, sid, id, lines); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, "UPDATE mealplan.meal_entries SET status='cooked',cooked_at=$3,actual_servings=$4,skipped_stock=$5,version=version+1 WHERE id=$1 AND space_id=$2", id, sid, s.db.Clock.Now(), servings, in.SkipStock); err != nil {
			return err
		}
		out.Status = "cooked"
		out.Version++
		return s.db.Activity(ctx, tx, sid, user, "meal.cooked", id, map[string]any{"servings": servings, "skipStock": in.SkipStock})
	})
	return out, err
}
func parseVersion(v string) (int64, error) {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, platform.Conflict()
	}
	return n, nil
}
