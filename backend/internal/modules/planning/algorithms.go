package planning

import (
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"math"
	"sort"
	"time"
	"unicode/utf8"
)

type Demand struct {
	FoodID, UnitID, Dimension string
	Quantity                  kernel.Quantity
	On                        time.Time
	Optional                  bool
}
type Supply struct {
	FoodID, Dimension      string
	Quantity               kernel.Quantity
	AvailableOn, ExpiresOn time.Time
}
type Weights struct{ Coverage, Expiry, Favorite float64 }
type RecipeScore struct {
	R, E, F, S float64
	Missing    int
}
type IngredientInput struct {
	FoodID   string          `json:"foodId"`
	UnitID   string          `json:"unitId"`
	Quantity kernel.Quantity `json:"quantity"`
	Optional bool            `json:"optional"`
}
type RecipeInput struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Servings    int               `json:"servings"`
	MealSlot    string            `json:"mealSlot,omitempty"`
	CookMinutes int               `json:"cookMinutes,omitempty"`
	Steps       []string          `json:"steps,omitempty"`
	Notes       string            `json:"notes,omitempty"`
	Ingredients []IngredientInput `json:"ingredients"`
}

func dateUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Missing allocates each valid source once in cooking-date order.
func Missing(demands []Demand, supply []Supply) ([]Demand, error) {
	d := append([]Demand(nil), demands...)
	s := append([]Supply(nil), supply...)
	for _, v := range d {
		if v.Quantity <= 0 {
			return nil, platform.Invalid("Lượng nguyên liệu phải dương")
		}
	}
	for _, v := range s {
		if v.Quantity < 0 {
			return nil, platform.Invalid("Tồn kho âm")
		}
	}
	sort.SliceStable(d, func(i, j int) bool {
		if dateUTC(d[i].On).Equal(dateUTC(d[j].On)) {
			return d[i].FoodID < d[j].FoodID
		}
		return dateUTC(d[i].On).Before(dateUTC(d[j].On))
	})
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].ExpiresOn.IsZero() {
			return false
		}
		if s[j].ExpiresOn.IsZero() {
			return true
		}
		return dateUTC(s[i].ExpiresOn).Before(dateUTC(s[j].ExpiresOn))
	})
	out := []Demand{}
	for _, need := range d {
		if need.Optional {
			continue
		}
		left := need.Quantity
		on := dateUTC(need.On)
		for i := range s {
			if s[i].FoodID != need.FoodID || s[i].Dimension != need.Dimension || s[i].Quantity == 0 {
				continue
			}
			if !s[i].AvailableOn.IsZero() && dateUTC(s[i].AvailableOn).After(on) {
				continue
			}
			if !s[i].ExpiresOn.IsZero() && dateUTC(s[i].ExpiresOn).Before(on) {
				continue
			}
			take := min(left, s[i].Quantity)
			s[i].Quantity -= take
			left -= take
			if left == 0 {
				break
			}
		}
		if left > 0 {
			need.Quantity = left
			out = append(out, need)
		}
	}
	return out, nil
}

// Score computes quantity coverage and expiry priority for one recipe.
func Score(needs []Demand, supply []Supply, today time.Time, reminderDays int, favorite bool, w Weights) (RecipeScore, error) {
	var out RecipeScore
	if reminderDays < 1 || reminderDays > 7 || math.Abs(w.Coverage+w.Expiry+w.Favorite-1) > 1e-9 || w.Coverage < 0 || w.Expiry < 0 || w.Favorite < 0 {
		return out, platform.Invalid("Tham số gợi ý không hợp lệ")
	}
	for _, v := range supply {
		if v.Quantity < 0 {
			return out, platform.Invalid("Tồn kho âm")
		}
	}
	today = dateUTC(today)
	required := 0
	enough := 0
	for _, need := range needs {
		if need.Quantity <= 0 {
			return out, platform.Invalid("Lượng nguyên liệu phải dương")
		}
		if need.Optional {
			continue
		}
		required++
		available := kernel.Quantity(0)
		soonest := time.Time{}
		for _, src := range supply {
			if src.FoodID != need.FoodID || src.Dimension != need.Dimension || src.Quantity <= 0 {
				continue
			}
			if !src.ExpiresOn.IsZero() && dateUTC(src.ExpiresOn).Before(today) {
				continue
			}
			if !src.AvailableOn.IsZero() && dateUTC(src.AvailableOn).After(today) {
				continue
			}
			if int64(available) > math.MaxInt64-int64(src.Quantity) {
				return out, platform.Invalid("Lượng vượt giới hạn")
			}
			available += src.Quantity
			if !src.ExpiresOn.IsZero() && (soonest.IsZero() || src.ExpiresOn.Before(soonest)) {
				soonest = src.ExpiresOn
			}
		}
		if available >= need.Quantity {
			enough++
		} else {
			out.Missing++
		}
		if !soonest.IsZero() {
			days := int(dateUTC(soonest).Sub(today).Hours() / 24)
			if days >= 0 && days <= reminderDays {
				out.E += float64(reminderDays+1-days) / float64(reminderDays+1)
			}
		}
	}
	if required > 0 {
		out.R = float64(enough) / float64(required)
	}
	out.E = math.Min(1, out.E)
	if favorite {
		out.F = 1
	}
	out.S = w.Coverage*out.R + w.Expiry*out.E + w.Favorite*out.F
	return out, nil
}

func validateRecipe(v RecipeInput) error {
	if n := utf8.RuneCountInString(v.Name); n < 2 || n > 100 {
		return platform.Invalid("Tên công thức dài 2–100 ký tự")
	}
	if v.Servings < 1 || v.Servings > 20 || len(v.Ingredients) < 1 || len(v.Ingredients) > 100 || v.CookMinutes < 0 || v.CookMinutes > 1440 {
		return platform.Invalid("Công thức không hợp lệ")
	}
	seen := map[string]bool{}
	for _, line := range v.Ingredients {
		if line.FoodID == "" || line.UnitID == "" || line.Quantity <= 0 || !line.Quantity.TwoDecimals() || seen[line.FoodID] {
			return platform.Invalid("Nguyên liệu không hợp lệ hoặc trùng")
		}
		seen[line.FoodID] = true
	}
	if v.MealSlot != "" && v.MealSlot != "breakfast" && v.MealSlot != "lunch" && v.MealSlot != "dinner" {
		return platform.Invalid("Bữa ăn không hợp lệ")
	}
	return nil
}
func validateMealDate(today, on time.Time, horizon int) error {
	today = dateUTC(today)
	on = dateUTC(on)
	if on.Before(today) || on.After(today.AddDate(0, 0, horizon)) {
		return platform.Invalid("Ngày ngoài khoảng lập kế hoạch")
	}
	return nil
}
