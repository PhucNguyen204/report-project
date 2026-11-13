package inventory

import (
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func filterValues(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

const expirySQL = `CASE WHEN l.expires_on<$5::date THEN 'expired' WHEN l.expires_on<=$5::date+$6::int THEN 'expiring' ELSE 'fresh' END`
const lotScopeSQL = ` FROM pantry.food_lots l JOIN catalog.foods f ON f.id=l.food_id WHERE l.space_id=$1 AND l.deleted_at IS NULL AND l.lifecycle='active'
 AND (cardinality($2::text[])=0 OR l.storage_location=ANY($2))
 AND (cardinality($3::text[])=0 OR (` + expirySQL + `)=ANY($3))
 AND (cardinality($4::text[])=0 OR COALESCE(f.category_id::text,'')=ANY($4))
 AND ($7='' OR strpos(lower(f.name),lower($7))>0)
 AND (NOT $8::boolean OR l.expires_on<=$5::date+$6::int)`

func (s *Service) listLots(w http.ResponseWriter, r *http.Request) error {
	if err := s.member(r); err != nil {
		return err
	}
	page, err := platform.ReadPage(r, platform.PageUUID)
	if err != nil {
		return err
	}
	locations := filters(r, "location")
	for value := range filters(r, "storageLocation") {
		locations[value] = true
	}
	today := s.db.Today()
	n := s.reminderDays(r.Context(), s.db.Pool, platform.Current(r).UserID)
	args := []any{r.PathValue("sid"), filterValues(locations), filterValues(filters(r, "expiryState")), filterValues(filters(r, "categoryId")), today, n, r.URL.Query().Get("q"), strings.HasSuffix(r.URL.Path, "/expiring")}
	facets := map[string]map[string]int{"location": {}, "expiryState": {}, "categoryId": {}}
	rows, err := s.db.Pool.Query(r.Context(), `SELECT l.storage_location,`+expirySQL+`,COALESCE(f.category_id::text,''),count(*)`+lotScopeSQL+` GROUP BY 1,2,3`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var location, state, category string
		var count int
		if err = rows.Scan(&location, &state, &category, &count); err != nil {
			rows.Close()
			return err
		}
		facets["location"][location] += count
		facets["expiryState"][state] += count
		facets["categoryId"][category] += count
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	args = append(args, page.After[0], page.Limit+1)
	rows, err = s.db.Pool.Query(r.Context(), `SELECT `+lotColumns+lotScopeSQL+` AND ($9='' OR l.id>NULLIF($9,'')::uuid) ORDER BY l.id LIMIT $10`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []Lot{}
	for rows.Next() {
		lot, err := scanLot(rows)
		if err != nil {
			return err
		}
		date, err := s.date(lot.ExpiresOn)
		if err != nil {
			return err
		}
		lot.ExpiryState = ExpiryState(date, today, n)
		out = append(out, lot)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	next := ""
	if len(out) > page.Limit {
		out = out[:page.Limit]
		next = page.Next(out[len(out)-1].ID)
	}
	platform.WritePage(w, map[string]any{"lots": out, "facets": facets, "nextCursor": next}, next, s.db.Clock.Now())
	return nil
}

type ListSummary struct {
	List
	ItemCount   int      `json:"itemCount"`
	PurchasedBy []string `json:"purchasedBy"`
}
type MonthSummary struct {
	Month     string `json:"month"`
	TotalCost int64  `json:"totalCost"`
	ListCount int    `json:"listCount"`
}

const listStatsSQL = ` FROM shopping.shopping_lists l LEFT JOIN LATERAL (
 SELECT count(*) AS item_count,COALESCE(sum(round(COALESCE(i.actual_quantity,i.quantity)*i.unit_price)) FILTER(WHERE i.status IN ('purchased','stocked') AND i.unit_price IS NOT NULL),0) AS total_cost,
 COALESCE(array_agg(DISTINCT i.purchased_by::text ORDER BY i.purchased_by::text) FILTER(WHERE i.status IN ('purchased','stocked') AND i.purchased_by IS NOT NULL),ARRAY[]::text[]) AS purchased_by
 FROM shopping.shopping_items i WHERE i.space_id=l.space_id AND i.list_id=l.id AND i.deleted_at IS NULL
 ) stats ON true WHERE l.space_id=$1 AND l.deleted_at IS NULL
 AND (cardinality($2::text[])=0 OR l.status=ANY($2))
 AND ($3='' OR l.planned_date>=NULLIF($3,'')::date) AND ($4='' OR l.planned_date<=NULLIF($4,'')::date)`

func scanListSummary(row pgx.Row) (ListSummary, error) {
	var out ListSummary
	var cost string
	err := row.Scan(&out.ID, &out.Name, &out.PlannedDate, &out.Status, &out.Notes, &out.Version, &out.ItemCount, &cost, &out.PurchasedBy)
	if err != nil {
		return out, err
	}
	out.TotalCost, err = strconv.ParseInt(cost, 10, 64)
	if err != nil {
		return out, platform.Invalid("Tổng tiền vượt giới hạn")
	}
	return out, nil
}
func (s *Service) listLists(w http.ResponseWriter, r *http.Request) error {
	if err := s.member(r); err != nil {
		return err
	}
	page, err := platform.ReadPage(r, platform.PageDate, platform.PageUUID)
	if err != nil {
		return err
	}
	from, to, group := r.URL.Query().Get("from"), r.URL.Query().Get("to"), r.URL.Query().Get("groupBy")
	for _, date := range []string{from, to} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return platform.Invalid("Ngày lọc không hợp lệ")
			}
		}
	}
	if from != "" && to != "" && from > to {
		return platform.Invalid("Khoảng ngày không hợp lệ")
	}
	if group != "" && group != "month" {
		return platform.Invalid("Cách nhóm không hợp lệ")
	}
	args := []any{r.PathValue("sid"), filterValues(filters(r, "status")), from, to}
	params := append(append([]any{}, args...), page.After[0], page.After[1], page.Limit+1)
	rows, err := s.db.Pool.Query(r.Context(), `SELECT l.id::text,l.name,to_char(l.planned_date,'YYYY-MM-DD'),l.status,l.notes,l.version,stats.item_count,stats.total_cost::text,stats.purchased_by`+listStatsSQL+`
 AND ($5='' OR l.planned_date<NULLIF($5,'')::date OR (l.planned_date=NULLIF($5,'')::date AND l.id>NULLIF($6,'')::uuid)) ORDER BY l.planned_date DESC,l.id LIMIT $7`, params...)
	if err != nil {
		return err
	}
	out := []ListSummary{}
	for rows.Next() {
		list, err := scanListSummary(rows)
		if err != nil {
			rows.Close()
			return err
		}
		out = append(out, list)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	next := ""
	if len(out) > page.Limit {
		out = out[:page.Limit]
		last := out[len(out)-1]
		next = page.Next(last.PlannedDate, last.ID)
	}
	data := map[string]any{"lists": out}
	if group == "month" {
		rows, err = s.db.Pool.Query(r.Context(), `SELECT to_char(l.planned_date,'YYYY-MM'),sum(stats.total_cost)::text,count(*)`+listStatsSQL+` GROUP BY 1 ORDER BY 1 DESC`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		months := []MonthSummary{}
		for rows.Next() {
			var month MonthSummary
			var cost string
			if err = rows.Scan(&month.Month, &cost, &month.ListCount); err != nil {
				return err
			}
			month.TotalCost, err = strconv.ParseInt(cost, 10, 64)
			if err != nil {
				return platform.Invalid("Tổng tiền vượt giới hạn")
			}
			months = append(months, month)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		data["months"] = months
	}
	platform.WritePage(w, data, next, s.db.Clock.Now())
	return nil
}
