package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

type SuggestionInput struct {
	Date            string
	MealSlot        string
	Servings, Limit int
	ExpiringOnly    bool
}
type SuggestedFood struct {
	FoodID   string `json:"foodId"`
	Name     string `json:"name"`
	DaysLeft int    `json:"daysLeft,omitempty"`
}
type SuggestedRecipe struct {
	Recipe       Recipe          `json:"recipe"`
	Score        float64         `json:"score"`
	R            float64         `json:"r"`
	E            float64         `json:"e"`
	F            float64         `json:"f"`
	Missing      []SuggestedFood `json:"missing"`
	ExpiringUsed []SuggestedFood `json:"expiringUsed"`
}
type SuggestionMeta struct {
	Fallback string  `json:"fallback"`
	Theta    float64 `json:"theta"`
}
type SuggestionResult struct {
	Items []SuggestedRecipe `json:"items"`
	Meta  SuggestionMeta    `json:"meta"`
}

func (s *Service) Suggest(ctx context.Context, user, sid string, in SuggestionInput) (SuggestionResult, error) {
	out := SuggestionResult{Items: []SuggestedRecipe{}, Meta: SuggestionMeta{Fallback: "none"}}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(ctx, tx, sid, user); err != nil {
			return err
		}
		h, err := s.horizon(ctx, tx)
		if err != nil {
			return err
		}
		on := s.db.Today()
		if in.Date != "" {
			on, err = parseDate(in.Date)
			if err != nil {
				return err
			}
		}
		if err = validateMealDate(s.db.Today(), on, h); err != nil {
			return err
		}
		if in.MealSlot != "" && in.MealSlot != "breakfast" && in.MealSlot != "lunch" && in.MealSlot != "dinner" {
			return platform.Invalid("Bữa ăn không hợp lệ")
		}
		if in.Servings == 0 {
			if err = tx.QueryRow(ctx, "SELECT count(*) FROM household.space_members WHERE space_id=$1", sid).Scan(&in.Servings); err != nil {
				return err
			}
		}
		if in.Servings < 1 || in.Servings > 20 {
			return platform.Invalid("Khẩu phần từ 1–20")
		}
		var maxResults int
		var theta float64
		var weightData []byte
		var reminder int
		if err = tx.QueryRow(ctx, "SELECT (SELECT (value#>>'{}')::int FROM admin.system_configs WHERE key='suggest.maxResults'),(SELECT (value#>>'{}')::float8 FROM admin.system_configs WHERE key='suggest.theta'),(SELECT value FROM admin.system_configs WHERE key='suggest.weights'),COALESCE((SELECT reminder_days FROM identity.user_settings WHERE user_id=$1),(SELECT (value#>>'{}')::int FROM admin.system_configs WHERE key='reminder.defaultDays'))", user).Scan(&maxResults, &theta, &weightData, &reminder); err != nil {
			return err
		}
		if in.Limit == 0 {
			in.Limit = maxResults
		}
		if in.Limit < 1 || in.Limit > maxResults {
			return platform.Invalid("Giới hạn gợi ý không hợp lệ")
		}
		var weights struct{ R, E, F float64 }
		if err = json.Unmarshal(weightData, &weights); err != nil {
			return err
		}
		out.Meta.Theta = theta
		if s.stock == nil {
			return platform.Err(503, 5003, "Dịch vụ tồn kho chưa sẵn sàng")
		}
		stocks, err := s.stock.Available(ctx, tx, sid, on)
		if err != nil {
			return err
		}
		supply := []Supply{}
		for _, stock := range stocks {
			supply = append(supply, Supply{FoodID: stock.FoodID, Dimension: stock.Dimension, Quantity: stock.Quantity, ExpiresOn: stock.ExpiresOn})
		}
		all, err := s.suggestionCandidates(ctx, tx, sid, user, in.MealSlot)
		if err != nil {
			return err
		}
		eligible := []SuggestedRecipe{}
		fallback := []SuggestedRecipe{}
		for _, c := range all {
			r := c.Recipe
			for i := range r.Ingredients {
				scaledQty, err := scaled(r.Ingredients[i].Quantity, in.Servings, r.Servings)
				if err != nil {
					return err
				}
				r.Ingredients[i].Quantity = scaledQty
			}
			demands := []Demand{}
			item := SuggestedRecipe{Recipe: r, Missing: []SuggestedFood{}, ExpiringUsed: []SuggestedFood{}}
			for _, need := range c.Needs {
				amount, err := scaled(need.Quantity, in.Servings, r.Servings)
				if err != nil {
					return err
				}
				need.Quantity = amount
				demands = append(demands, Demand{FoodID: need.FoodID, UnitID: need.UnitID, Dimension: need.Dimension, Quantity: need.Quantity, Optional: need.Optional, On: on})
				name := need.Name
				var available kernel.Quantity
				var soonest time.Time
				for _, stock := range stocks {
					if stock.FoodID == need.FoodID && stock.Dimension == need.Dimension {
						available += stock.Quantity
						if soonest.IsZero() || stock.ExpiresOn.Before(soonest) {
							soonest = stock.ExpiresOn
						}
					}
				}
				if !need.Optional && available < need.Quantity {
					item.Missing = append(item.Missing, SuggestedFood{FoodID: need.FoodID, Name: name})
				}
				if !soonest.IsZero() {
					days := int(dateUTC(soonest).Sub(dateUTC(on)).Hours() / 24)
					if days >= 0 && days <= reminder {
						item.ExpiringUsed = append(item.ExpiringUsed, SuggestedFood{FoodID: need.FoodID, Name: name, DaysLeft: days})
					}
				}
			}
			score, err := Score(demands, supply, on, reminder, c.Favorite, Weights{weights.R, weights.E, weights.F})
			if err != nil {
				return err
			}
			item.R, item.E, item.F, item.Score = score.R, score.E, score.F, score.S
			if len(stocks) == 0 {
				if r.Kind == "template" {
					eligible = append(eligible, item)
				}
				continue
			}
			if in.ExpiringOnly && len(item.ExpiringUsed) == 0 {
				continue
			}
			fallback = append(fallback, item)
			if item.R >= theta {
				eligible = append(eligible, item)
			}
		}
		less := func(a, b SuggestedRecipe) bool {
			if a.Score != b.Score {
				return a.Score > b.Score
			}
			if a.Recipe.Name != b.Recipe.Name {
				return a.Recipe.Name < b.Recipe.Name
			}
			return a.Recipe.ID < b.Recipe.ID
		}
		sort.Slice(eligible, func(i, j int) bool { return less(eligible[i], eligible[j]) })
		if len(stocks) == 0 {
			out.Meta.Fallback = "empty_pantry"
		} else if len(eligible) == 0 {
			out.Meta.Fallback = "below_threshold"
			sort.Slice(fallback, func(i, j int) bool {
				if fallback[i].R != fallback[j].R {
					return fallback[i].R > fallback[j].R
				}
				return less(fallback[i], fallback[j])
			})
			eligible = fallback
			if len(eligible) > 3 {
				eligible = eligible[:3]
			}
		}
		if len(eligible) > in.Limit {
			eligible = eligible[:in.Limit]
		}
		out.Items = eligible
		return nil
	})
	return out, err
}

type suggestionNeed struct {
	FoodID    string          `json:"foodId"`
	UnitID    string          `json:"unitId"`
	Dimension string          `json:"dimension"`
	Name      string          `json:"name"`
	Quantity  kernel.Quantity `json:"quantity"`
	Optional  bool            `json:"optional"`
}
type suggestionCandidate struct {
	Recipe   Recipe           `json:"recipe"`
	Needs    []suggestionNeed `json:"needs"`
	Favorite bool             `json:"favorite"`
}

func (s *Service) suggestionCandidates(ctx context.Context, tx pgx.Tx, sid, user, slot string) ([]suggestionCandidate, error) {
	rows, err := tx.Query(ctx, `SELECT jsonb_build_object('recipe',jsonb_build_object('id',r.id::text,'name',r.name,'servings',r.servings,'kind',r.kind,'status',r.status,'mealSlot',r.meal_slot,'cookMinutes',r.cook_minutes,'steps',r.steps,'notes',r.notes,'imageObjectId',r.image_object_id::text,'version',r.version,'ingredients',jsonb_agg(jsonb_build_object('foodId',i.food_id::text,'unitId',i.unit_id::text,'quantity',i.quantity,'optional',i.optional) ORDER BY i.food_id)), 'favorite',EXISTS(SELECT 1 FROM recipe.favorite_recipes f WHERE f.user_id=$2 AND f.recipe_id=r.id), 'needs',jsonb_agg(jsonb_build_object('foodId',i.food_id::text,'unitId',i.unit_id::text,'dimension',i.dimension,'name',food.name,'quantity',i.quantity*i.unit_factor,'optional',i.optional) ORDER BY i.food_id)) FROM recipe.recipes r JOIN recipe.recipe_ingredients i ON i.recipe_id=r.id JOIN catalog.foods food ON food.id=i.food_id WHERE r.deleted_at IS NULL AND r.status='published' AND (r.space_id=$1 OR r.kind='template') AND ($3='' OR r.meal_slot='' OR r.meal_slot=$3) GROUP BY r.id ORDER BY r.id`, sid, user, slot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []suggestionCandidate{}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		var c suggestionCandidate
		if err = json.Unmarshal(b, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
