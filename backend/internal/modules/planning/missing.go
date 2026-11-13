package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"math"
	"sort"
	"time"
	"unicode/utf8"
)

type ConfirmLine struct {
	FoodID   string          `json:"foodId"`
	UnitID   string          `json:"unitId"`
	Quantity kernel.Quantity `json:"quantity"`
}
type GenerateInput struct {
	From         string        `json:"from"`
	To           string        `json:"to"`
	DryRun       bool          `json:"dryRun"`
	TargetListID string        `json:"targetListId,omitempty"`
	PlannedDate  string        `json:"plannedDate,omitempty"`
	Name         string        `json:"name,omitempty"`
	Lines        []ConfirmLine `json:"lines,omitempty"`
}
type RecipeMissingInput struct {
	Servings     int           `json:"servings"`
	DryRun       bool          `json:"dryRun"`
	TargetListID string        `json:"targetListId,omitempty"`
	PlannedDate  string        `json:"plannedDate,omitempty"`
	Name         string        `json:"name,omitempty"`
	Lines        []ConfirmLine `json:"lines,omitempty"`
}
type MissingMeal struct {
	MealID   string `json:"mealId"`
	MealDate string `json:"mealDate"`
	Name     string `json:"name"`
}
type MissingPreviewLine struct {
	FoodID   string          `json:"foodId"`
	Name     string          `json:"name"`
	ShortQty kernel.Quantity `json:"shortQty"`
	UnitID   string          `json:"unitId"`
	NeededBy string          `json:"neededBy"`
	Meals    []MissingMeal   `json:"meals"`
}
type MissingResult struct {
	Lines                []MissingPreviewLine `json:"lines,omitempty"`
	SkippedMeals         []MissingMeal        `json:"skippedMeals,omitempty"`
	SuggestedPlannedDate string               `json:"suggestedPlannedDate,omitempty"`
	ListID               string               `json:"listId,omitempty"`
}

func (s *Service) confirmMissing(ctx context.Context, tx pgx.Tx, user, sid, name, target, date string, lines []ConfirmLine) (string, error) {
	composer, ok := s.lists.(platform.PlannedListComposer)
	if !ok {
		return "", platform.Err(503, 5003, "Dịch vụ danh sách chưa sẵn sàng")
	}
	if (target == "") == (date == "") {
		return "", platform.Invalid("Chọn danh sách đích hoặc ngày mua")
	}
	if len(lines) < 1 || len(lines) > 100 || utf8.RuneCountInString(name) > 100 {
		return "", platform.Invalid("Danh sách nguyên liệu không hợp lệ")
	}
	var planned time.Time
	var err error
	if date != "" {
		planned, err = parseDate(date)
		if err != nil {
			return "", err
		}
		if dateUTC(planned).Before(dateUTC(s.db.Today())) {
			return "", platform.Invalid("Ngày mua ở quá khứ")
		}
	}
	out := []platform.MissingLine{}
	seen := map[string]bool{}
	for _, line := range lines {
		if line.Quantity <= 0 || !line.Quantity.TwoDecimals() || seen[line.FoodID] {
			return "", platform.Invalid("Lượng nguyên liệu không hợp lệ hoặc trùng")
		}
		seen[line.FoodID] = true
		u, err := s.catalog.ValidateFood(ctx, tx, sid, line.FoodID, line.UnitID)
		if err != nil {
			return "", err
		}
		out = append(out, platform.MissingLine{FoodID: u.FoodID, UnitID: u.UnitID, Name: u.Name, Quantity: line.Quantity})
	}
	return composer.ComposePlanned(ctx, tx, sid, user, platform.Composition{TargetListID: target, Name: name, PlannedDate: planned, Lines: out})
}

func (s *Service) previewMissing(ctx context.Context, tx pgx.Tx, sid string, demands []Demand, meals map[string][]MissingMeal, skipped []MissingMeal, pending bool, from, to time.Time) (MissingResult, error) {
	out := MissingResult{Lines: []MissingPreviewLine{}, SkippedMeals: skipped}
	if s.stock == nil {
		return out, platform.Err(503, 5003, "Dịch vụ tồn kho chưa sẵn sàng")
	}
	stocks, err := s.stock.Available(ctx, tx, sid, s.db.Today())
	if err != nil {
		return out, err
	}
	supply := []Supply{}
	for _, stock := range stocks {
		supply = append(supply, Supply{FoodID: stock.FoodID, Dimension: stock.Dimension, Quantity: stock.Quantity, ExpiresOn: stock.ExpiresOn})
	}
	if pending {
		port, ok := s.lists.(platform.PendingSupplyPort)
		if !ok {
			return out, platform.Err(503, 5003, "Dịch vụ hàng đang mua chưa sẵn sàng")
		}
		items, err := port.Pending(ctx, tx, sid, from, to)
		if err != nil {
			return out, err
		}
		for _, item := range items {
			supply = append(supply, Supply{FoodID: item.FoodID, Dimension: item.Dimension, Quantity: item.Quantity, AvailableOn: item.AvailableOn})
		}
	}
	missing, err := Missing(demands, supply)
	if err != nil {
		return out, err
	}
	grouped := map[string]*MissingPreviewLine{}
	baseAmounts := map[string]kernel.Quantity{}
	for _, need := range missing {
		key := need.FoodID + ":" + need.Dimension
		line := grouped[key]
		if line == nil {
			u, err := s.catalog.ValidateFood(ctx, tx, sid, need.FoodID, need.UnitID)
			if err != nil {
				return out, err
			}
			line = &MissingPreviewLine{FoodID: need.FoodID, Name: u.Name, UnitID: need.UnitID, NeededBy: need.On.Format("2006-01-02"), Meals: []MissingMeal{}}
			grouped[key] = line
		}
		if int64(baseAmounts[key]) > math.MaxInt64-int64(need.Quantity) {
			return out, platform.Invalid("Lượng vượt giới hạn")
		}
		baseAmounts[key] += need.Quantity
		line.Meals = append(line.Meals, meals[need.FoodID+":"+need.On.Format("2006-01-02")]...)
	}
	for key, line := range grouped {
		u, err := s.catalog.ValidateFood(ctx, tx, sid, line.FoodID, line.UnitID)
		if err != nil {
			return out, err
		}
		display, err := baseAmounts[key].Div(u.Factor)
		if err != nil || !display.TwoDecimals() {
			if err = tx.QueryRow(ctx, "SELECT id::text FROM catalog.units WHERE dimension=$1 AND is_base", u.Dimension).Scan(&line.UnitID); err != nil {
				return out, err
			}
			display = baseAmounts[key]
		}
		if !display.TwoDecimals() {
			return out, platform.Invalid("Lượng thiếu không biểu diễn được bằng hai chữ số thập phân")
		}
		line.ShortQty = display
		out.Lines = append(out.Lines, *line)
	}
	sort.Slice(out.Lines, func(i, j int) bool {
		if out.Lines[i].NeededBy != out.Lines[j].NeededBy {
			return out.Lines[i].NeededBy < out.Lines[j].NeededBy
		}
		return out.Lines[i].FoodID < out.Lines[j].FoodID
	})
	planned := dateUTC(s.db.Today())
	if len(out.Lines) > 0 {
		earliest, _ := parseDate(out.Lines[0].NeededBy)
		if before := earliest.AddDate(0, 0, -1); before.After(planned) {
			planned = before
		}
	}
	out.SuggestedPlannedDate = planned.Format("2006-01-02")
	return out, nil
}

func (s *Service) Generate(ctx context.Context, user, sid string, in GenerateInput) (MissingResult, error) {
	var out MissingResult
	from, err := parseDate(in.From)
	if err != nil {
		return out, err
	}
	to, err := parseDate(in.To)
	if err != nil {
		return out, err
	}
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		h, err := s.horizon(ctx, tx)
		if err != nil {
			return err
		}
		if err = validateMealDate(s.db.Today(), from, h); err != nil {
			return err
		}
		if err = validateMealDate(s.db.Today(), to, h); err != nil {
			return err
		}
		if to.Before(from) {
			return platform.Invalid("Khoảng ngày không hợp lệ")
		}
		if !in.DryRun {
			out.ListID, err = s.confirmMissing(ctx, tx, user, sid, in.Name, in.TargetListID, in.PlannedDate, in.Lines)
			return err
		}
		rows, err := tx.Query(ctx, "SELECT id::text,recipe_id::text,name,meal_date,servings FROM mealplan.meal_entries WHERE space_id=$1 AND meal_date BETWEEN $2 AND $3 AND status='planned' AND deleted_at IS NULL ORDER BY meal_date,id", sid, from, to)
		if err != nil {
			return err
		}
		type candidate struct {
			id, name string
			rid      *string
			on       time.Time
			servings int
		}
		candidates := []candidate{}
		for rows.Next() {
			var c candidate
			if err = rows.Scan(&c.id, &c.rid, &c.name, &c.on, &c.servings); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, c)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		demands := []Demand{}
		meals := map[string][]MissingMeal{}
		skipped := []MissingMeal{}
		for _, c := range candidates {
			meal := MissingMeal{MealID: c.id, MealDate: c.on.Format("2006-01-02"), Name: c.name}
			if c.rid == nil {
				skipped = append(skipped, meal)
				continue
			}
			needs, err := s.normalizedIngredients(ctx, tx, sid, *c.rid, kernel.Quantity(int64(c.servings)*kernel.Scale))
			if err != nil {
				return err
			}
			for _, line := range needs {
				demands = append(demands, Demand{FoodID: line.FoodID, UnitID: line.DisplayUnit, Dimension: line.Dimension, Quantity: line.Quantity, On: c.on, Optional: line.Optional})
				meals[line.FoodID+":"+c.on.Format("2006-01-02")] = append(meals[line.FoodID+":"+c.on.Format("2006-01-02")], meal)
			}
		}
		out, err = s.previewMissing(ctx, tx, sid, demands, meals, skipped, true, from, to)
		return err
	})
	return out, err
}

func (s *Service) MissingRecipe(ctx context.Context, user, sid, rid string, in RecipeMissingInput) (MissingResult, error) {
	var out MissingResult
	if in.Servings < 1 || in.Servings > 20 {
		return out, platform.Invalid("Khẩu phần từ 1–20")
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		needs, err := s.normalizedIngredients(ctx, tx, sid, rid, kernel.Quantity(int64(in.Servings)*kernel.Scale))
		if err != nil {
			return err
		}
		if !in.DryRun {
			out.ListID, err = s.confirmMissing(ctx, tx, user, sid, in.Name, in.TargetListID, in.PlannedDate, in.Lines)
			return err
		}
		demands := []Demand{}
		for _, line := range needs {
			demands = append(demands, Demand{FoodID: line.FoodID, UnitID: line.DisplayUnit, Dimension: line.Dimension, Quantity: line.Quantity, On: s.db.Today(), Optional: line.Optional})
		}
		out, err = s.previewMissing(ctx, tx, sid, demands, map[string][]MissingMeal{}, []MissingMeal{}, false, s.db.Today(), s.db.Today())
		return err
	})
	return out, err
}
