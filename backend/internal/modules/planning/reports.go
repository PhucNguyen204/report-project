package planning

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"math"
	"sort"
	"time"
)

type ReportPoint struct {
	Period     string                     `json:"period"`
	TotalCost  int64                      `json:"totalCost"`
	Quantities map[string]kernel.Quantity `json:"quantities"`
}
type ReportFood struct {
	FoodID    string          `json:"foodId"`
	Name      string          `json:"name"`
	Dimension string          `json:"dimension"`
	Quantity  kernel.Quantity `json:"quantity"`
	TotalCost int64           `json:"totalCost"`
}
type ReportReason struct {
	Reason    string `json:"reason"`
	Lots      int    `json:"lots"`
	TotalCost int64  `json:"totalCost"`
}
type ReportCategory struct {
	CategoryID string                     `json:"categoryId"`
	Name       string                     `json:"name"`
	TotalCost  int64                      `json:"totalCost"`
	Quantities map[string]kernel.Quantity `json:"quantities"`
}

type ReportSummary struct {
	From         string                     `json:"from"`
	To           string                     `json:"to"`
	TotalCost    int64                      `json:"totalCost"`
	Quantities   map[string]kernel.Quantity `json:"quantities"`
	Lots         int                        `json:"lots"`
	UnpricedLots int                        `json:"unpricedLots"`
	CookedMeals  int                        `json:"cookedMeals"`
	WasteRate    float64                    `json:"wasteRate"`
}
type ReportChange struct {
	TotalCost    int64                      `json:"totalCost"`
	Quantities   map[string]kernel.Quantity `json:"quantities"`
	Lots         int                        `json:"lots"`
	UnpricedLots int                        `json:"unpricedLots"`
	CookedMeals  int                        `json:"cookedMeals"`
	WasteRate    float64                    `json:"wasteRate"`
}

type ReportResult struct {
	Previous     ReportSummary              `json:"previous"`
	Change       ReportChange               `json:"change"`
	Categories   []ReportCategory           `json:"categories"`
	Type         string                     `json:"type"`
	From         string                     `json:"from"`
	To           string                     `json:"to"`
	GroupBy      string                     `json:"groupBy"`
	TotalCost    int64                      `json:"totalCost"`
	Quantities   map[string]kernel.Quantity `json:"quantities"`
	UnpricedLots int                        `json:"unpricedLots"`
	Lots         int                        `json:"lots"`
	CookedMeals  int                        `json:"cookedMeals"`
	WasteRate    float64                    `json:"wasteRate"`
	Empty        bool                       `json:"empty"`
	Series       []ReportPoint              `json:"series"`
	TopFoods     []ReportFood               `json:"topFoods"`
	Reasons      []ReportReason             `json:"reasons"`
}
type reportRow struct {
	ID, LotID, FoodID, Name, Dimension, Quantity, Reason, CategoryID, CategoryName string
	Cost                                                                           int64
	Priced                                                                         bool
	OccurredAt                                                                     time.Time
}

func safeCost(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, platform.Invalid("Giá trị vượt giới hạn")
	}
	return a + b, nil
}
func reportPeriod(on time.Time, group string) string {
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	on = on.In(loc)
	switch group {
	case "week":
		on = on.AddDate(0, 0, -(int(on.Weekday())+6)%7)
	case "month":
		on = time.Date(on.Year(), on.Month(), 1, 0, 0, 0, 0, loc)
	}
	return on.Format("2006-01-02")
}

func (s *Service) Report(ctx context.Context, user, sid, kind string, from, to time.Time, groupBy string) (ReportResult, error) {
	out := ReportResult{Type: kind, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), GroupBy: groupBy, Quantities: map[string]kernel.Quantity{}, Series: []ReportPoint{}, TopFoods: []ReportFood{}, Reasons: []ReportReason{}}
	if from.IsZero() || to.IsZero() || dateUTC(to).Before(dateUTC(from)) || !dateUTC(to).Before(dateUTC(from).AddDate(0, 12, 0)) {
		return out, platform.Invalid("Khoảng báo cáo tối đa12 tháng")
	}
	if groupBy != "day" && groupBy != "week" && groupBy != "month" {
		return out, platform.Invalid("Cách nhóm không hợp lệ")
	}
	var source string
	switch kind {
	case "spending":
		source = `SELECT v.id::text AS id,v.id::text AS lotid,v.food_id::text AS foodid,v.name,v.dimension,COALESCE(v.category_id::text,'') AS categoryid,COALESCE(v.category_name,'Khác') AS categoryname,(v.actual_quantity*v.unit_factor)::text AS quantity,CASE WHEN v.unit_price IS NULL THEN 0 ELSE round(v.actual_quantity*v.unit_price)::bigint END AS cost,(v.unit_price IS NOT NULL) AS priced,v.purchased_at AS occurredat,'' AS reason FROM report.v_spending v WHERE v.space_id=$1 AND v.purchased_at >= $2 AND v.purchased_at < $3`
	case "consumption", "waste":
		movement := "consume"
		if kind == "waste" {
			movement = "discard"
		}
		source = `SELECT v.id::text AS id,v.lot_id::text AS lotid,v.food_id::text AS foodid,v.name,v.dimension,COALESCE(v.category_id::text,'') AS categoryid,COALESCE(v.category_name,'Khác') AS categoryname,v.quantity::text AS quantity,CASE WHEN v.total_cost IS NULL THEN 0 ELSE round(v.total_cost*v.quantity/(v.initial_qty*v.unit_factor))::bigint END AS cost,(v.total_cost IS NOT NULL) AS priced,v.occurred_at AS occurredat,v.reason FROM report.v_movements v WHERE v.space_id=$1 AND v.occurred_at >= $2 AND v.occurred_at < $3 AND v.kind='` + movement + `'`
	default:
		return out, platform.Invalid("Loại báo cáo không hợp lệ")
	}
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(ctx, tx, sid, user); err != nil {
			return err
		}
		var data, previousData []byte
		var imported, previousImported int
		previousStart := start.AddDate(0, 0, -int(dateUTC(to).Sub(dateUTC(from))/(24*time.Hour))-1)
		out.Previous = ReportSummary{From: previousStart.Format("2006-01-02"), To: start.AddDate(0, 0, -1).Format("2006-01-02"), Quantities: map[string]kernel.Quantity{}}
		out.Change.Quantities = map[string]kernel.Quantity{}
		sql := `WITH src AS (` + source + `) SELECT COALESCE(jsonb_agg(to_jsonb(src)) FILTER(WHERE occurredat >= $4),'[]'::jsonb),COALESCE(jsonb_agg(to_jsonb(src)) FILTER(WHERE occurredat < $4),'[]'::jsonb),(SELECT count(DISTINCT lot_id) FROM report.v_movements WHERE space_id=$1 AND occurred_at >= $4 AND occurred_at < $3 AND kind='stock_in'),(SELECT count(*) FROM mealplan.meal_entries WHERE space_id=$1 AND cooked_at >= $4 AND cooked_at < $3 AND status='cooked'),(SELECT count(DISTINCT lot_id) FROM report.v_movements WHERE space_id=$1 AND occurred_at >= $2 AND occurred_at < $4 AND kind='stock_in'),(SELECT count(*) FROM mealplan.meal_entries WHERE space_id=$1 AND cooked_at >= $2 AND cooked_at < $4 AND status='cooked') FROM src`
		if err := tx.QueryRow(ctx, sql, sid, previousStart, end, start).Scan(&data, &previousData, &imported, &out.CookedMeals, &previousImported, &out.Previous.CookedMeals); err != nil {
			return err
		}
		var rows []reportRow
		if err := json.Unmarshal(data, &rows); err != nil {
			return err
		}
		var previousRows []reportRow
		if err := json.Unmarshal(previousData, &previousRows); err != nil {
			return err
		}
		previousLots, previousUnpriced := map[string]bool{}, map[string]bool{}
		for _, row := range previousRows {
			amount, err := kernel.ParseQuantity(row.Quantity)
			if err != nil {
				return err
			}
			if amount < 0 {
				amount = -amount
			}
			sum, err := safeCost(int64(out.Previous.Quantities[row.Dimension]), int64(amount))
			if err != nil {
				return err
			}
			out.Previous.Quantities[row.Dimension] = kernel.Quantity(sum)
			out.Previous.TotalCost, err = safeCost(out.Previous.TotalCost, row.Cost)
			if err != nil {
				return err
			}
			previousLots[row.LotID] = true
			if !row.Priced {
				previousUnpriced[row.LotID] = true
			}
		}
		out.Previous.Lots = len(previousLots)
		out.Previous.UnpricedLots = len(previousUnpriced)
		if kind == "waste" && previousImported > 0 {
			out.Previous.WasteRate = float64(out.Previous.Lots) / float64(previousImported)
		}
		out.Empty = len(rows) == 0
		lots := map[string]bool{}
		unpriced := map[string]bool{}
		series := map[string]*ReportPoint{}
		foods := map[string]*ReportFood{}
		categories := map[string]*ReportCategory{}
		reasons := map[string]*ReportReason{}
		reasonLots := map[string]map[string]bool{}
		for _, row := range rows {
			amount, err := kernel.ParseQuantity(row.Quantity)
			if err != nil {
				return err
			}
			if amount < 0 {
				amount = -amount
			}
			if int64(out.Quantities[row.Dimension]) > math.MaxInt64-int64(amount) {
				return platform.Invalid("Lượng vượt giới hạn")
			}
			out.Quantities[row.Dimension] += amount
			out.TotalCost, err = safeCost(out.TotalCost, row.Cost)
			if err != nil {
				return err
			}
			lots[row.LotID] = true
			if !row.Priced {
				unpriced[row.LotID] = true
			}
			category := categories[row.CategoryID]
			if category == nil {
				category = &ReportCategory{CategoryID: row.CategoryID, Name: row.CategoryName, Quantities: map[string]kernel.Quantity{}}
				categories[row.CategoryID] = category
			}
			category.Quantities[row.Dimension] += amount
			category.TotalCost, err = safeCost(category.TotalCost, row.Cost)
			if err != nil {
				return err
			}
			period := reportPeriod(row.OccurredAt, groupBy)
			point := series[period]
			if point == nil {
				point = &ReportPoint{Period: period, Quantities: map[string]kernel.Quantity{}}
				series[period] = point
			}
			point.TotalCost, err = safeCost(point.TotalCost, row.Cost)
			if err != nil {
				return err
			}
			point.Quantities[row.Dimension] += amount
			key := row.FoodID + ":" + row.Dimension
			food := foods[key]
			if food == nil {
				food = &ReportFood{FoodID: row.FoodID, Name: row.Name, Dimension: row.Dimension}
				foods[key] = food
			}
			food.Quantity += amount
			food.TotalCost, err = safeCost(food.TotalCost, row.Cost)
			if err != nil {
				return err
			}
			if kind == "waste" {
				reason := reasons[row.Reason]
				if reason == nil {
					reason = &ReportReason{Reason: row.Reason}
					reasons[row.Reason] = reason
					reasonLots[row.Reason] = map[string]bool{}
				}
				reason.TotalCost, err = safeCost(reason.TotalCost, row.Cost)
				if err != nil {
					return err
				}
				reasonLots[row.Reason][row.LotID] = true
			}
		}
		out.Lots = len(lots)
		out.UnpricedLots = len(unpriced)
		if kind == "waste" && imported > 0 {
			out.WasteRate = float64(out.Lots) / float64(imported)
		}
		out.Change.TotalCost = out.TotalCost - out.Previous.TotalCost
		out.Change.Lots = out.Lots - out.Previous.Lots
		out.Change.UnpricedLots = out.UnpricedLots - out.Previous.UnpricedLots
		out.Change.CookedMeals = out.CookedMeals - out.Previous.CookedMeals
		out.Change.WasteRate = out.WasteRate - out.Previous.WasteRate
		for dimension, quantity := range out.Quantities {
			out.Change.Quantities[dimension] = quantity - out.Previous.Quantities[dimension]
		}
		for dimension, quantity := range out.Previous.Quantities {
			if _, ok := out.Quantities[dimension]; !ok {
				out.Change.Quantities[dimension] = -quantity
			}
		}
		for _, v := range categories {
			out.Categories = append(out.Categories, *v)
		}
		sort.Slice(out.Categories, func(i, j int) bool { return out.Categories[i].CategoryID < out.Categories[j].CategoryID })
		for _, v := range series {
			out.Series = append(out.Series, *v)
		}
		sort.Slice(out.Series, func(i, j int) bool { return out.Series[i].Period < out.Series[j].Period })
		for _, v := range foods {
			out.TopFoods = append(out.TopFoods, *v)
		}
		sort.Slice(out.TopFoods, func(i, j int) bool {
			if out.TopFoods[i].Quantity != out.TopFoods[j].Quantity {
				return out.TopFoods[i].Quantity > out.TopFoods[j].Quantity
			}
			return out.TopFoods[i].FoodID < out.TopFoods[j].FoodID
		})
		limit := 10
		if kind == "waste" {
			limit = 5
		}
		if len(out.TopFoods) > limit {
			out.TopFoods = out.TopFoods[:limit]
		}
		for key, v := range reasons {
			v.Lots = len(reasonLots[key])
			out.Reasons = append(out.Reasons, *v)
		}
		sort.Slice(out.Reasons, func(i, j int) bool { return out.Reasons[i].Reason < out.Reasons[j].Reason })
		return nil
	})
	return out, err
}
