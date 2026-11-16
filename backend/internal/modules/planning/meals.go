package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strconv"
	"strings"
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
	ID           string     `json:"id"`
	RecipeID     *string    `json:"recipeId"`
	Name         string     `json:"name"`
	Date         string     `json:"date"`
	MealSlot     string     `json:"mealSlot"`
	Servings     int        `json:"servings"`
	Status       string     `json:"status"`
	Version      int64      `json:"version"`
	CookedAt     *time.Time `json:"cookedAt"`
	CookedBy     *string    `json:"cookedBy"`
	SkippedStock bool       `json:"skippedStock"`
}
type CookResult struct {
	Meal
	Details       Meal                      `json:"meal"`
	Deductions    []platform.StockDeduction `json:"deductions"`
	AlreadyCooked bool                      `json:"alreadyCooked"`
}
type CookLine struct {
	FoodID       string          `json:"foodId"`
	UnitID       string          `json:"unitId"`
	Quantity     kernel.Quantity `json:"quantity"`
	LotID        string          `json:"lotId,omitempty"`
	LotIDs       []string        `json:"lotIds,omitempty"`
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
	if in.ID != "" && !platform.UUID7(in.ID) {
		return out, platform.Invalid("id phải là UUIDv7")
	}
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
	sql := "SELECT id::text,recipe_id::text,name,meal_date::text,meal_slot,servings,status,version,cooked_at,cooked_by::text,skipped_stock FROM mealplan.meal_entries WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL"
	if lock {
		sql += " FOR UPDATE"
	}
	err := q.QueryRow(ctx, sql, id, sid).Scan(&m.ID, &m.RecipeID, &m.Name, &m.Date, &m.MealSlot, &m.Servings, &m.Status, &m.Version, &m.CookedAt, &m.CookedBy, &m.SkippedStock)
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
func (s *Service) Cook(ctx context.Context, user, sid, id string, in CookInput) (CookResult, error) {
	var out CookResult
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		m, err := readMeal(ctx, tx, sid, id, true)
		if err != nil {
			return err
		}
		if m.Status == "cooked" {
			out, err = s.cookResult(ctx, tx, sid, m, true)
			return err
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
			allowed := map[string]string{}
			for _, ingredient := range ingredients {
				allowed[ingredient.FoodID] = ingredient.Dimension
				if !ingredient.Optional {
					lines = append(lines, platform.Consumption{FoodID: ingredient.FoodID, Dimension: ingredient.Dimension, Quantity: ingredient.Quantity})
				}
			}
			if in.Lines != nil {
				lines = []platform.Consumption{}
				seen := map[string]bool{}
				for _, line := range in.Lines {
					if !validCookUUID(line.FoodID) {
						return platform.Invalid("Thực phẩm nguyên liệu không hợp lệ")
					}
					if !validCookUUID(line.UnitID) {
						return platform.Invalid("Đơn vị nguyên liệu không hợp lệ")
					}
					u, err := s.catalog.ValidateFood(ctx, tx, sid, line.FoodID, line.UnitID)
					if err != nil {
						return err
					}
					if allowed[u.FoodID] != u.Dimension || seen[u.FoodID] || line.Quantity < 0 || line.Quantity > kernel.Quantity(9_999_999_999_990_000) || !line.Quantity.TwoDecimals() {
						return platform.Invalid("Dòng trừ kho không hợp lệ")
					}
					seen[u.FoodID] = true
					if line.Quantity == 0 && (line.LotID != "" || len(line.LotIDs) > 0) {
						return platform.Invalid("Nguyên liệu lấy ngoài tủ không chọn lô")
					}
					if len(line.LotIDs) > 100 || (line.LotID != "" && len(line.LotIDs) > 0) {
						return platform.Invalid("Lựa chọn lô không hợp lệ")
					}
					selected := make([]string, 0, len(line.LotIDs))
					selectedSet := map[string]bool{}
					for _, id := range line.LotIDs {
						canonical := strings.ToLower(id)
						if !validCookUUID(id) || selectedSet[canonical] {
							return platform.Invalid("Lựa chọn lô không hợp lệ")
						}
						selectedSet[canonical] = true
						selected = append(selected, canonical)
					}
					if line.Quantity == 0 {
						continue
					}
					quantity, err := line.Quantity.Mul(u.Factor)
					if err != nil {
						return err
					}
					lines = append(lines, platform.Consumption{FoodID: u.FoodID, Dimension: u.Dimension, LotID: line.LotID, LotIDs: selected, Quantity: quantity, AllowExpired: line.AllowExpired})
				}
			}
			if s.stock == nil {
				return platform.Err(503, 5003, "Dịch vụ tồn kho chưa sẵn sàng")
			}
			if _, ok := s.stock.(platform.StockHistoryPort); !ok {
				return platform.Err(503, 5003, "Dịch vụ lịch sử tồn kho chưa sẵn sàng")
			}
			consumeTx, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			defer consumeTx.Rollback(context.WithoutCancel(ctx))
			if err = s.stock.Consume(platform.WithTransaction(ctx, consumeTx), consumeTx, sid, id, lines); err != nil {
				if rollbackErr := consumeTx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil {
					return rollbackErr
				}
				var failure *platform.Error
				if errors.As(err, &failure) && failure.Status == 422 && failure.Code == 2003 {
					stocks, snapshotErr := s.stock.Available(platform.WithTransaction(ctx, tx), tx, sid, s.db.Today())
					if snapshotErr != nil {
						return snapshotErr
					}
					current := []platform.StockLevel{}
					for _, stock := range stocks {
						if allowed[stock.FoodID] == stock.Dimension {
							current = append(current, platform.StockLevel{FoodID: stock.FoodID, LotID: stock.LotID, Dimension: stock.Dimension, BaseQuantity: stock.Quantity})
						}
					}
					return &platform.StockConflictError{Current: current}
				}
				return err
			}
			if err = consumeTx.Commit(ctx); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, "UPDATE mealplan.meal_entries SET status='cooked',cooked_at=$3,cooked_by=$6,actual_servings=$4,skipped_stock=$5,version=version+1 WHERE id=$1 AND space_id=$2", id, sid, s.db.Clock.Now(), servings, in.SkipStock, user); err != nil {
			return err
		}
		if err = s.db.Activity(ctx, tx, sid, user, "meal.cooked", id, map[string]any{"servings": servings, "skipStock": in.SkipStock}); err != nil {
			return err
		}
		m, err = readMeal(ctx, tx, sid, id, false)
		if err != nil {
			return err
		}
		out, err = s.cookResult(ctx, tx, sid, m, false)
		return err
	})
	return out, err
}
func validCookUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	var parsed pgtype.UUID
	return parsed.Scan(id) == nil
}
func (s *Service) cookResult(ctx context.Context, tx pgx.Tx, sid string, meal Meal, replay bool) (CookResult, error) {
	out := CookResult{Meal: meal, Details: meal, Deductions: []platform.StockDeduction{}, AlreadyCooked: replay}
	if meal.SkippedStock {
		return out, nil
	}
	history, ok := s.stock.(platform.StockHistoryPort)
	if !ok {
		return CookResult{}, platform.Err(503, 5003, "Dịch vụ lịch sử tồn kho chưa sẵn sàng")
	}
	deductions, err := history.MealDeductions(ctx, tx, sid, meal.ID)
	if err != nil {
		return CookResult{}, err
	}
	if deductions != nil {
		out.Deductions = deductions
	}
	return out, nil
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
