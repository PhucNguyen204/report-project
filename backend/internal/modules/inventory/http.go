package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Service) Register(m *http.ServeMux) {
	base := "/it4788/v1/spaces/{sid}"
	m.HandleFunc("POST "+base+"/lots", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var c LotInput
		if err := platform.Decode(r, &c); err != nil {
			return err
		}
		out, err := s.CreateLot(r.Context(), r.PathValue("sid"), platform.Current(r).UserID, c)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	m.HandleFunc("GET "+base+"/lots", platform.Handle(s.listLots))
	m.HandleFunc("GET "+base+"/lots/expiring", platform.Handle(s.listLots))
	m.HandleFunc("GET "+base+"/lots/{lotId}", platform.Handle(s.getLot))
	m.HandleFunc("PATCH "+base+"/lots/{lotId}", platform.Handle(s.patchLot))
	m.HandleFunc("POST "+base+"/lots/{lotId}/consume", platform.Handle(s.consumeLot))
	m.HandleFunc("POST "+base+"/lots/{lotId}/discard", platform.Handle(s.discardLot))
	m.HandleFunc("POST "+base+"/shopping-lists", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var c ListInput
		if err := platform.Decode(r, &c); err != nil {
			return err
		}
		out, err := s.CreateList(r.Context(), r.PathValue("sid"), platform.Current(r).UserID, c)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	m.HandleFunc("GET "+base+"/shopping-lists", platform.Handle(s.listLists))
	m.HandleFunc("GET "+base+"/shopping-lists/{listId}", platform.Handle(s.getList))
	m.HandleFunc("PATCH "+base+"/shopping-lists/{listId}", platform.Handle(s.patchList))
	m.HandleFunc("DELETE "+base+"/shopping-lists/{listId}", platform.Handle(s.deleteList))
	m.HandleFunc("POST "+base+"/shopping-lists/{listId}/copy", platform.Handle(s.copyList))
	m.HandleFunc("POST "+base+"/shopping-lists/{listId}/items", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var c ItemInput
		if err := platform.Decode(r, &c); err != nil {
			return err
		}
		out, err := s.AddItem(r.Context(), r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("listId"), c)
		if err != nil {
			return err
		}
		platform.Write(w, 201, out)
		return nil
	}))
	m.HandleFunc("PATCH "+base+"/shopping-items/{itemId}", platform.Handle(s.patchItem))
	m.HandleFunc("DELETE "+base+"/shopping-items/{itemId}", platform.Handle(s.deleteItem))
	m.HandleFunc("PUT "+base+"/shopping-items/{itemId}/assignee", platform.Handle(s.assignItem))
	m.HandleFunc("DELETE "+base+"/shopping-items/{itemId}/assignee", platform.Handle(s.assignItem))
	m.HandleFunc("POST "+base+"/shopping-items/purchase", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var c struct {
			Items []PurchaseInput `json:"items"`
		}
		if err := platform.Decode(r, &c); err != nil {
			return err
		}
		out, err := s.Purchase(r.Context(), r.PathValue("sid"), platform.Current(r).UserID, c.Items)
		if err != nil {
			return err
		}
		platform.Write(w, 200, map[string]any{"results": out})
		return nil
	}))
	m.HandleFunc("POST "+base+"/shopping-items/{itemId}/undo-purchase", platform.Handle(s.undoPurchase))
	m.HandleFunc("POST "+base+"/shopping-items/stock", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		var c struct {
			Items []StockItemInput `json:"items"`
		}
		if err := platform.Decode(r, &c); err != nil {
			return err
		}
		out, err := s.StockItems(r.Context(), r.PathValue("sid"), platform.Current(r).UserID, c.Items)
		if err != nil {
			return err
		}
		platform.Write(w, 200, map[string]any{"results": out})
		return nil
	}))
}

func (s *Service) member(r *http.Request) error {
	if platform.Current(r).SystemRole == "admin" {
		return platform.NotFound()
	}
	_, err := s.db.RequireMember(r.Context(), s.db.Pool, r.PathValue("sid"), platform.Current(r).UserID)
	return err
}
func version(r *http.Request) (int64, error) {
	text := r.Header.Get("If-Match")
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	if text == "" || strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, platform.Invalid("If-Match bắt buộc")
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil || v < 1 {
		return 0, platform.Invalid("If-Match bắt buộc")
	}
	return v, nil
}
func chosen(filter map[string]bool, value string) bool { return len(filter) == 0 || filter[value] }
func filters(r *http.Request, key string) map[string]bool {
	out := map[string]bool{}
	for _, v := range r.URL.Query()[key] {
		for _, part := range strings.Split(v, ",") {
			if part != "" {
				out[part] = true
			}
		}
	}
	return out
}
func limit(r *http.Request) (int, error) {
	if r.URL.Query().Get("limit") == "" {
		return 50, nil
	}
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 1 || n > 100 {
		return 0, platform.Invalid("limit phải từ 1 đến 100")
	}
	return n, nil
}

func (s *Service) listLots(w http.ResponseWriter, r *http.Request) error {
	if err := s.member(r); err != nil {
		return err
	}
	nmax, err := limit(r)
	if err != nil {
		return err
	}
	sid := r.PathValue("sid")
	uid := platform.Current(r).UserID
	n := s.reminderDays(r.Context(), s.db.Pool, uid)
	rows, err := s.db.Pool.Query(r.Context(), `SELECT `+lotColumns+`,COALESCE(f.category_id::text,'') FROM pantry.food_lots l JOIN catalog.foods f ON f.id=l.food_id WHERE l.space_id=$1 AND l.deleted_at IS NULL ORDER BY l.id`, sid)
	if err != nil {
		return err
	}
	defer rows.Close()
	locations, states, categories := filters(r, "location"), filters(r, "expiryState"), filters(r, "categoryId")
	for v := range filters(r, "storageLocation") {
		locations[v] = true
	}
	cursor := r.URL.Query().Get("cursor")
	q := strings.ToLower(r.URL.Query().Get("q"))
	out := make([]Lot, 0)
	facets := map[string]map[string]int{"location": {}, "expiryState": {}, "categoryId": {}}
	for rows.Next() {
		var lot Lot
		var category string
		if err := rows.Scan(&lot.ID, &lot.FoodID, &lot.Name, &lot.UnitID, &lot.Dimension, &lot.InitialQty, &lot.RemainingQty, &lot.StorageLocation, &lot.PurchasedOn, &lot.ExpiresOn, &lot.Lifecycle, &lot.Notes, &lot.Version, &lot.TotalCost, &category); err != nil {
			return err
		}
		date, err := s.date(lot.ExpiresOn)
		if err != nil {
			return err
		}
		lot.ExpiryState = ExpiryState(date, s.db.Today(), n)
		if lot.Lifecycle != "active" || !chosen(locations, lot.StorageLocation) || !chosen(states, lot.ExpiryState) || !chosen(categories, category) || !strings.Contains(strings.ToLower(lot.Name), q) {
			continue
		}
		if strings.HasSuffix(r.URL.Path, "/expiring") && lot.ExpiryState == "fresh" {
			continue
		}
		facets["location"][lot.StorageLocation]++
		facets["expiryState"][lot.ExpiryState]++
		facets["categoryId"][category]++
		if lot.ID > cursor {
			out = append(out, lot)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	next := ""
	if len(out) > nmax {
		next = out[nmax-1].ID
		out = out[:nmax]
	}
	platform.Write(w, 200, map[string]any{"lots": out, "facets": facets, "nextCursor": next})
	return nil
}
func (s *Service) getLot(w http.ResponseWriter, r *http.Request) error {
	if err := s.member(r); err != nil {
		return err
	}
	lot, err := scanLot(s.db.Pool.QueryRow(r.Context(), `SELECT `+lotColumns+` FROM pantry.food_lots l JOIN catalog.foods f ON f.id=l.food_id WHERE l.id=$1 AND l.space_id=$2 AND l.deleted_at IS NULL`, r.PathValue("lotId"), r.PathValue("sid")))
	if err != nil {
		return err
	}
	d, err := s.date(lot.ExpiresOn)
	if err != nil {
		return err
	}
	lot.ExpiryState = ExpiryState(d, s.db.Today(), s.reminderDays(r.Context(), s.db.Pool, platform.Current(r).UserID))
	rows, err := s.db.Pool.Query(r.Context(), `SELECT m.id::text,m.kind,m.quantity::text,m.reason,m.meal_id::text,m.occurred_at,u.id::text FROM pantry.stock_movements m JOIN catalog.units u ON u.dimension=$3 AND u.is_base WHERE m.space_id=$1 AND m.lot_id=$2 ORDER BY m.occurred_at,m.id LIMIT 1000`, r.PathValue("sid"), lot.ID, lot.Dimension)
	if err != nil {
		return err
	}
	defer rows.Close()
	movements := []Movement{}
	for rows.Next() {
		var movement Movement
		if err = rows.Scan(&movement.ID, &movement.Kind, &movement.Quantity, &movement.Reason, &movement.MealID, &movement.OccurredAt, &movement.UnitID); err != nil {
			return err
		}
		movements = append(movements, movement)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", lot.Version))
	platform.Write(w, 200, struct {
		Lot
		Movements []Movement `json:"movements"`
	}{lot, movements})
	return nil
}

type Movement struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Quantity   kernel.Quantity `json:"quantity"`
	UnitID     string          `json:"unitId"`
	Reason     string          `json:"reason"`
	MealID     *string         `json:"mealId,omitempty"`
	OccurredAt time.Time       `json:"occurredAt"`
}

type lotPatch struct {
	RemainingQuantity *kernel.Quantity `json:"remainingQuantity,omitempty"`
	RemainingQty      *kernel.Quantity `json:"remainingQty,omitempty"`
	StorageLocation   *string          `json:"storageLocation,omitempty"`
	ExpiresOn         *string          `json:"expiresOn,omitempty"`
	Notes             *string          `json:"notes,omitempty"`
}

func (s *Service) patchLot(w http.ResponseWriter, r *http.Request) error {
	var c lotPatch
	if err := platform.Decode(r, &c); err != nil {
		return err
	}
	if c.RemainingQty != nil {
		if c.RemainingQuantity != nil && *c.RemainingQuantity != *c.RemainingQty {
			return platform.Invalid("Lượng còn lại không nhất quán")
		}
		c.RemainingQuantity = c.RemainingQty
	}
	base, err := version(r)
	if err != nil {
		return err
	}
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("lotId")
	var out Lot
	overwritten := []string{}
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		var factor kernel.Quantity
		lot, err := scanLot(tx.QueryRow(r.Context(), `SELECT `+lotColumns+` FROM pantry.food_lots l JOIN catalog.foods f ON f.id=l.food_id WHERE l.id=$1 AND l.space_id=$2 AND l.deleted_at IS NULL FOR UPDATE OF l`, id, sid))
		if err != nil {
			return err
		}
		patch := map[string]any{}
		if c.StorageLocation != nil {
			patch["storageLocation"] = *c.StorageLocation
		}
		if c.ExpiresOn != nil {
			patch["expiresOn"] = *c.ExpiresOn
		}
		if c.Notes != nil {
			patch["notes"] = *c.Notes
		}
		overwritten, err = overwrittenFields(r.Context(), tx, "pantry.food_lots", id, base, lot.Version, patch, map[string]any{"storageLocation": lot.StorageLocation, "expiresOn": lot.ExpiresOn, "notes": lot.Notes})
		if err != nil {
			return err
		}
		if c.RemainingQuantity != nil {
			if base != lot.Version {
				return platform.Conflict()
			}
			amount := *c.RemainingQuantity
			if amount < 0 || !amount.TwoDecimals() || amount > lot.InitialQty {
				return platform.Invalid("Lượng còn lại không hợp lệ")
			}
			lot.RemainingQty = amount
			if amount == 0 {
				lot.Lifecycle = "consumed"
			} else {
				lot.Lifecycle = "active"
			}
		}
		if c.StorageLocation != nil {
			if !validLocation(*c.StorageLocation) {
				return platform.Invalid("Vị trí không hợp lệ")
			}
			lot.StorageLocation = *c.StorageLocation
		}
		if c.ExpiresOn != nil {
			expiry, err := s.date(*c.ExpiresOn)
			if err != nil || expiry.Format("2006-01-02") < lot.PurchasedOn {
				return platform.Invalid("Ngày hết hạn không hợp lệ")
			}
			lot.ExpiresOn = *c.ExpiresOn
		}
		if c.Notes != nil {
			if utf8.RuneCountInString(*c.Notes) > 200 {
				return platform.Invalid("Ghi chú quá dài")
			}
			lot.Notes = *c.Notes
		}
		var oldBase kernel.Quantity
		if err := tx.QueryRow(r.Context(), `SELECT unit_factor::text,base_remaining::text FROM pantry.food_lots WHERE id=$1 AND space_id=$2`, id, sid).Scan(&factor, &oldBase); err != nil {
			return err
		}
		base, err := lot.RemainingQty.Mul(factor)
		if err != nil {
			return platform.Invalid("Lượng không biểu diễn chính xác")
		}
		_, err = tx.Exec(r.Context(), `UPDATE pantry.food_lots SET remaining_qty=$3,base_remaining=$4,lifecycle=$5,storage_location=$6,expires_on=$7,notes=$8,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, lot.RemainingQty.String(), base.String(), lot.Lifecycle, lot.StorageLocation, lot.ExpiresOn, lot.Notes)
		if err != nil {
			return err
		}
		if c.RemainingQuantity != nil && base != oldBase {
			if _, err := tx.Exec(r.Context(), `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,actor_id,occurred_at) VALUES($1,$2,$3,$4,'adjust',$5,$6,$7)`, platform.ID(), sid, id, lot.FoodID, (base - oldBase).String(), uid, s.db.Clock.Now()); err != nil {
				return err
			}
		}
		lot.Version++
		lot.ExpiryState = ExpiryState(mustDate(lot.ExpiresOn), s.db.Today(), s.reminderDays(r.Context(), tx, uid))
		out = lot
		return s.db.Activity(r.Context(), tx, sid, uid, "lot.updated", id, out)
	})
	if err != nil {
		return err
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, out.Version))
	platform.Write(w, 200, struct {
		Lot
		OverwrittenFields []string `json:"overwrittenFields"`
	}{out, overwritten})
	return nil
}
func mustDate(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
func (s *Service) consumeLot(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Quantity kernel.Quantity `json:"quantity"`
		UnitID   string          `json:"unitId"`
		Strategy string          `json:"strategy"`
	}
	if err := platform.Decode(r, &c); err != nil {
		return err
	}
	if err := validQuantity(c.Quantity); err != nil {
		return err
	}
	if c.Strategy != "" && c.Strategy != "fefo" && c.Strategy != "lot" {
		return platform.Invalid("Chiến lược không hợp lệ")
	}
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("lotId")
	var fid string
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		var dimension string
		if err := tx.QueryRow(r.Context(), `SELECT food_id::text,dimension FROM pantry.food_lots WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL`, id, sid).Scan(&fid, &dimension); err != nil {
			return err
		}
		food, err := s.catalog.ValidateFood(r.Context(), tx, sid, fid, c.UnitID)
		if err != nil {
			return err
		}
		if food.Dimension != dimension {
			return platform.Invalid("BR16: Đơn vị khác nhóm đại lượng")
		}
		base, err := c.Quantity.Mul(food.Factor)
		if err != nil {
			return platform.Invalid("Không thể quy đổi chính xác")
		}
		explicit := ""
		if c.Strategy == "lot" {
			explicit = id
		}
		if explicit == "" {
			var expiredOnly bool
			if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM pantry.food_lots WHERE space_id=$1 AND food_id=$2 AND lifecycle='active') AND NOT EXISTS(SELECT 1 FROM pantry.food_lots WHERE space_id=$1 AND food_id=$2 AND lifecycle='active' AND expires_on>=$3::date)`, sid, fid, s.db.Today().Format("2006-01-02")).Scan(&expiredOnly); err != nil {
				return err
			}
			if expiredOnly {
				return platform.Rule("BR15: Cần chọn rõ lô đã hết hạn")
			}
		}
		if err = s.Consume(r.Context(), tx, sid, "", []platform.Consumption{{FoodID: fid, LotID: explicit, Quantity: base, AllowExpired: c.Strategy == "lot"}}); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "lot.consumed", id, c)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"foodId": fid})
	return nil
}
func (s *Service) discardLot(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Quantity kernel.Quantity `json:"quantity"`
		Reason   string          `json:"reason"`
	}
	if err := platform.Decode(r, &c); err != nil {
		return err
	}
	if err := validQuantity(c.Quantity); err != nil {
		return err
	}
	if strings.TrimSpace(c.Reason) == "" || utf8.RuneCountInString(c.Reason) > 200 {
		return platform.Invalid("Cần nhập lý do loại bỏ")
	}
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("lotId")
	var remaining kernel.Quantity
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		var food string
		var factor kernel.Quantity
		var state string
		if err := tx.QueryRow(r.Context(), `SELECT food_id::text,remaining_qty::text,unit_factor::text,lifecycle FROM pantry.food_lots WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, sid).Scan(&food, &remaining, &factor, &state); err != nil {
			return err
		}
		if state != "active" || c.Quantity > remaining {
			return platform.Err(422, 2003, "Lượng loại bỏ vượt lượng còn lại")
		}
		remaining -= c.Quantity
		base, err := remaining.Mul(factor)
		if err != nil {
			return err
		}
		delta, err := c.Quantity.Mul(factor)
		if err != nil {
			return err
		}
		life := "active"
		if remaining == 0 {
			life = "discarded"
		}
		if _, err = tx.Exec(r.Context(), `UPDATE pantry.food_lots SET remaining_qty=$3,base_remaining=$4,lifecycle=$5,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, remaining.String(), base.String(), life); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,reason,actor_id,occurred_at) VALUES($1,$2,$3,$4,'discard',$5,$6,$7,$8)`, platform.ID(), sid, id, food, delta.String(), c.Reason, uid, s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "lot.discarded", id, c)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"remainingQuantity": remaining})
	return nil
}

func (s *Service) listLists(w http.ResponseWriter, r *http.Request) error {
	if err := s.member(r); err != nil {
		return err
	}
	nmax, err := limit(r)
	if err != nil {
		return err
	}
	rows, err := s.db.Pool.Query(r.Context(), `SELECT `+listColumns+` FROM shopping.shopping_lists WHERE space_id=$1 AND deleted_at IS NULL ORDER BY planned_date DESC,id`, r.PathValue("sid"))
	if err != nil {
		return err
	}
	defer rows.Close()
	out := make([]List, 0)
	statuses := filters(r, "status")
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return err
		}
		if !chosen(statuses, l.Status) || (from != "" && l.PlannedDate < from) || (to != "" && l.PlannedDate > to) {
			continue
		}
		out = append(out, l)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(out) > nmax {
		out = out[:nmax]
	}
	platform.Write(w, 200, map[string]any{"lists": out})
	return nil
}
func (s *Service) getList(w http.ResponseWriter, r *http.Request) error {
	if err := s.member(r); err != nil {
		return err
	}
	sid, id := r.PathValue("sid"), r.PathValue("listId")
	l, err := scanList(s.db.Pool.QueryRow(r.Context(), `SELECT `+listColumns+` FROM shopping.shopping_lists WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL`, id, sid))
	if err != nil {
		return err
	}
	rows, err := s.db.Pool.Query(r.Context(), `SELECT `+itemColumns+` FROM shopping.shopping_items i WHERE list_id=$1 AND space_id=$2 AND deleted_at IS NULL ORDER BY id`, id, sid)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		i, err := scanItem(rows)
		if err != nil {
			return err
		}
		items = append(items, i)
		if i.UnitPrice != nil && (i.Status == "purchased" || i.Status == "stocked") {
			amount := i.Quantity
			if i.ActualQuantity != nil {
				amount = *i.ActualQuantity
			}
			cost, err := totalCost(amount, *i.UnitPrice)
			if err != nil {
				return err
			}
			if l.TotalCost > 9223372036854775807-cost {
				return platform.Invalid("Tổng tiền vượt giới hạn")
			}
			l.TotalCost += cost
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", l.Version))
	platform.Write(w, 200, map[string]any{"list": l, "items": items})
	return nil
}
func (s *Service) patchList(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Name        *string `json:"name"`
		PlannedDate *string `json:"plannedDate"`
		Notes       *string `json:"notes"`
	}
	if err := platform.Decode(r, &c); err != nil {
		return err
	}
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("listId")
	var out List
	overwritten := []string{}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		l, err := s.lockList(r.Context(), tx, sid, id, false)
		if err != nil {
			return err
		}
		v, err := version(r)
		if err != nil {
			return err
		}
		patch := map[string]any{}
		if c.Name != nil {
			patch["name"] = *c.Name
		}
		if c.Notes != nil {
			patch["notes"] = *c.Notes
		}
		if c.PlannedDate != nil {
			patch["plannedDate"] = *c.PlannedDate
		}
		overwritten, err = overwrittenFields(r.Context(), tx, "shopping.shopping_lists", id, v, l.Version, patch, map[string]any{"name": l.Name, "notes": l.Notes, "plannedDate": l.PlannedDate})
		if err != nil {
			return err
		}
		if c.Name != nil {
			if n := utf8.RuneCountInString(strings.TrimSpace(*c.Name)); n < 1 || n > 100 {
				return platform.Invalid("Tên không hợp lệ")
			}
			l.Name = *c.Name
		}
		if c.Notes != nil {
			if utf8.RuneCountInString(*c.Notes) > 200 {
				return platform.Invalid("Ghi chú quá dài")
			}
			l.Notes = *c.Notes
		}
		if c.PlannedDate != nil {
			d, err := s.date(*c.PlannedDate)
			if err != nil || d.Before(s.db.Today()) {
				return platform.Invalid("Ngày mua không hợp lệ")
			}
			l.PlannedDate = *c.PlannedDate
		}
		if _, err = tx.Exec(r.Context(), `UPDATE shopping.shopping_lists SET name=$3,planned_date=$4,notes=$5,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, l.Name, l.PlannedDate, l.Notes); err != nil {
			return err
		}
		l.Version++
		out = l
		return s.db.Activity(r.Context(), tx, sid, uid, "shopping.list.updated", id, l)
	})
	if err != nil {
		return err
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, out.Version))
	platform.Write(w, 200, struct {
		List
		OverwrittenFields []string `json:"overwrittenFields"`
	}{out, overwritten})
	return nil
}
func (s *Service) deleteList(w http.ResponseWriter, r *http.Request) error {
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("listId")
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		if _, err := s.lockList(r.Context(), tx, sid, id, false); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE shopping.shopping_items SET deleted_at=$3,version=version+1 WHERE list_id=$1 AND space_id=$2`, id, sid, s.db.Clock.Now()); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE shopping.shopping_lists SET deleted_at=$3,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "shopping.list.deleted", id, nil)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]bool{"deleted": true})
	return nil
}
func (s *Service) copyList(w http.ResponseWriter, r *http.Request) error {
	var c ListInput
	if r.ContentLength != 0 {
		if err := platform.Decode(r, &c); err != nil {
			return err
		}
	}
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("listId")
	var newID string
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		source, err := s.lockList(r.Context(), tx, sid, id, true)
		if err != nil {
			return err
		}
		if c.Name == "" {
			c.Name = source.Name
		}
		if n := utf8.RuneCountInString(c.Name); n < 1 || n > 100 {
			return platform.Invalid("Tên không hợp lệ")
		}
		d, err := s.date(c.PlannedDate)
		if err != nil || d.Before(s.db.Today()) {
			return platform.Invalid("Ngày mua không hợp lệ")
		}
		newID = platform.ID()
		if _, err = tx.Exec(r.Context(), `INSERT INTO shopping.shopping_lists(id,space_id,name,planned_date,notes) VALUES($1,$2,$3,$4,$5)`, newID, sid, c.Name, d, source.Notes); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), `SELECT `+itemColumns+` FROM shopping.shopping_items i WHERE list_id=$1 AND space_id=$2 AND deleted_at IS NULL ORDER BY id`, id, sid)
		if err != nil {
			return err
		}
		items := []Item{}
		for rows.Next() {
			i, err := scanItem(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, i)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, i := range items {
			var food any
			if i.FoodID != nil {
				food = *i.FoodID
			}
			if _, err = tx.Exec(r.Context(), `INSERT INTO shopping.shopping_items(id,space_id,list_id,food_id,name,unit_id,unit_factor,dimension,quantity,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, platform.ID(), sid, newID, food, i.Name, i.UnitID, i.UnitFactor.String(), i.Dimension, i.Quantity.String(), i.Notes); err != nil {
				return err
			}
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "shopping.list.copied", newID, map[string]string{"sourceId": id})
	})
	if err != nil {
		return err
	}
	platform.Write(w, 201, map[string]string{"id": newID})
	return nil
}

func (s *Service) patchItem(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Name           *string          `json:"name"`
		Quantity       *kernel.Quantity `json:"quantity"`
		ActualQuantity *kernel.Quantity `json:"actualQuantity"`
		UnitPrice      *int64           `json:"unitPrice"`
		Notes          *string          `json:"notes"`
		Status         *string          `json:"status"`
	}
	if err := platform.Decode(r, &c); err != nil {
		return err
	}
	base, err := version(r)
	if err != nil {
		return err
	}
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("itemId")
	var out Item
	overwritten := []string{}
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		items, err := s.lockItems(r.Context(), tx, sid, []string{id})
		if err != nil {
			return err
		}
		i := items[id]
		patch := map[string]any{}
		if c.Name != nil {
			patch["name"] = *c.Name
		}
		if c.Quantity != nil {
			patch["quantity"] = *c.Quantity
		}
		if c.ActualQuantity != nil {
			patch["actualQuantity"] = *c.ActualQuantity
		}
		if c.UnitPrice != nil {
			patch["unitPrice"] = *c.UnitPrice
		}
		if c.Notes != nil {
			patch["notes"] = *c.Notes
		}
		if c.Status != nil {
			patch["status"] = *c.Status
		}
		overwritten, err = overwrittenFields(r.Context(), tx, "shopping.shopping_items", id, base, i.Version, patch, map[string]any{"name": i.Name, "quantity": i.Quantity, "actualQuantity": i.ActualQuantity, "unitPrice": i.UnitPrice, "notes": i.Notes, "status": i.Status})
		if err != nil {
			return err
		}
		if i.Status == "stocked" {
			return platform.Rule("Mặt hàng đã nhập tủ chỉ đọc")
		}
		l, err := s.lockList(r.Context(), tx, sid, i.ListID, true)
		if err != nil {
			return err
		}
		if l.Status == "completed" && (c.Name != nil || c.Quantity != nil || c.Notes != nil || c.Status != nil) {
			return platform.Rule("BR08: Danh sách đã hoàn tất")
		}
		if c.Quantity != nil {
			if err := validQuantity(*c.Quantity); err != nil {
				return err
			}
			i.Quantity = *c.Quantity
		}
		if c.ActualQuantity != nil {
			if i.Status != "purchased" {
				return platform.Rule("Chỉ sửa lượng thực tế khi đã mua")
			}
			if err := validQuantity(*c.ActualQuantity); err != nil {
				return err
			}
			i.ActualQuantity = c.ActualQuantity
		}
		if c.UnitPrice != nil {
			if *c.UnitPrice < 0 {
				return platform.Invalid("Đơn giá không hợp lệ")
			}
			i.UnitPrice = c.UnitPrice
		}
		if c.Name != nil {
			if i.FoodID != nil {
				return platform.Invalid("Không đổi tên thực phẩm danh mục")
			}
			if n := utf8.RuneCountInString(*c.Name); n < 1 || n > 100 {
				return platform.Invalid("Tên không hợp lệ")
			}
			i.Name = *c.Name
		}
		if c.Notes != nil {
			if utf8.RuneCountInString(*c.Notes) > 200 {
				return platform.Invalid("Ghi chú quá dài")
			}
			i.Notes = *c.Notes
		}
		if c.Status != nil {
			if *c.Status != "skipped" || (i.Status != "to_buy" && i.Status != "assigned") {
				return platform.Rule("Chuyển trạng thái không hợp lệ")
			}
			i.Status = "skipped"
			i.AssigneeID = nil
		}
		var actual any
		if i.ActualQuantity != nil {
			actual = i.ActualQuantity.String()
		}
		_, err = tx.Exec(r.Context(), `UPDATE shopping.shopping_items SET name=$3,quantity=$4,actual_quantity=$5,unit_price=$6,notes=$7,status=$8,assignee_id=$9,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, i.Name, i.Quantity.String(), actual, i.UnitPrice, i.Notes, i.Status, i.AssigneeID)
		if err != nil {
			return err
		}
		i.Version++
		out = i
		if err = s.recalculate(r.Context(), tx, sid, i.ListID); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "shopping.item.updated", id, out)
	})
	if err != nil {
		return err
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, out.Version))
	platform.Write(w, 200, struct {
		Item
		OverwrittenFields []string `json:"overwrittenFields"`
	}{out, overwritten})
	return nil
}
func (s *Service) deleteItem(w http.ResponseWriter, r *http.Request) error {
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("itemId")
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		items, err := s.lockItems(r.Context(), tx, sid, []string{id})
		if err != nil {
			return err
		}
		i := items[id]
		if _, err = s.lockList(r.Context(), tx, sid, i.ListID, false); err != nil {
			return err
		}
		if i.Status == "stocked" {
			return platform.Rule("Mặt hàng đã nhập tủ chỉ đọc")
		}
		if _, err = tx.Exec(r.Context(), `UPDATE shopping.shopping_items SET deleted_at=$3,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, s.db.Clock.Now()); err != nil {
			return err
		}
		if err = s.recalculate(r.Context(), tx, sid, i.ListID); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "shopping.item.deleted", id, nil)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]bool{"deleted": true})
	return nil
}
func (s *Service) undoPurchase(w http.ResponseWriter, r *http.Request) error {
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("itemId")
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(r.Context(), tx, sid, uid); err != nil {
			return err
		}
		items, err := s.lockItems(r.Context(), tx, sid, []string{id})
		if err != nil {
			return err
		}
		i := items[id]
		if i.Status != "purchased" {
			return platform.Rule("Chỉ hoàn tác mặt hàng đã mua chưa nhập tủ")
		}
		state := "to_buy"
		if i.AssigneeID != nil {
			state = "assigned"
		}
		if _, err = tx.Exec(r.Context(), `UPDATE shopping.shopping_items SET status=$3,actual_quantity=NULL,purchased_by=NULL,purchased_at=NULL,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, state); err != nil {
			return err
		}
		if err = s.recalculate(r.Context(), tx, sid, i.ListID); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `UPDATE shopping.shopping_lists SET status='shopping' WHERE id=$1 AND space_id=$2`, i.ListID, sid); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "shopping.purchase.undone", id, nil)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]bool{"undone": true})
	return nil
}
func (s *Service) assignItem(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		UserID string `json:"userId"`
	}
	if r.Method == "PUT" {
		if err := platform.Decode(r, &c); err != nil {
			return err
		}
		if c.UserID == "" {
			return platform.Invalid("Cần chọn người mua")
		}
	}
	sid, uid, id := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("itemId")
	var out Item
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtext($1))`, sid); err != nil {
			return err
		}
		role, err := s.db.LockMember(r.Context(), tx, sid, uid)
		if err != nil {
			return err
		}
		if role != "leader" {
			return platform.Forbidden()
		}
		if c.UserID != "" {
			if _, err := s.db.LockMember(r.Context(), tx, sid, c.UserID); err != nil {
				return platform.Rule("Người được giao không còn thuộc nhóm")
			}
		}
		items, err := s.lockItems(r.Context(), tx, sid, []string{id})
		if err != nil {
			return err
		}
		i := items[id]
		if _, err = s.lockList(r.Context(), tx, sid, i.ListID, false); err != nil {
			return err
		}
		v, err := version(r)
		if err != nil {
			return err
		}
		if v != i.Version {
			return platform.Conflict()
		}
		if i.Status != "to_buy" && i.Status != "assigned" {
			return platform.Rule("BR10: Mặt hàng không thể phân công")
		}
		state := "to_buy"
		var assignee any
		previousRecipient := ""
		if i.AssigneeID != nil {
			previousRecipient = *i.AssigneeID
		}
		recipient := c.UserID
		if c.UserID != "" {
			state = "assigned"
			assignee = c.UserID
		} else if i.AssigneeID != nil {
			recipient = *i.AssigneeID
		}
		if _, err = tx.Exec(r.Context(), `UPDATE shopping.shopping_items SET assignee_id=$3,status=$4,version=version+1 WHERE id=$1 AND space_id=$2`, id, sid, assignee, state); err != nil {
			return err
		}
		i.Status = state
		i.AssigneeID = nil
		if c.UserID != "" {
			i.AssigneeID = &c.UserID
		}
		i.Version++
		out = i
		recipients := map[string]bool{recipient: true, previousRecipient: true}
		for target := range recipients {
			if target != "" && target != uid {
				if err = s.db.Enqueue(r.Context(), tx, "assignment.digest", target+":"+i.ListID, map[string]any{"userId": target, "listId": i.ListID, "spaceId": sid, "batchId": platform.ID()}, s.db.Clock.Now().Add(5*time.Second)); err != nil {
					return err
				}
			}
		}

		return s.db.Activity(r.Context(), tx, sid, uid, "shopping.assigned", id, out)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, out)
	return nil
}

func (s *Service) HandleAssignmentDigest(ctx context.Context, e platform.Event) error {
	var p struct {
		UserID  string `json:"userId"`
		ListID  string `json:"listId"`
		SpaceID string `json:"spaceId"`
		BatchID string `json:"batchId"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(ctx, tx, p.SpaceID, p.UserID); err != nil {
			var app *platform.Error
			if errors.As(err, &app) && app.Status == 404 {
				return nil
			}
			return err
		}
		var count int
		var name string
		if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(max(l.name),'') FROM shopping.shopping_items i JOIN shopping.shopping_lists l ON l.id=i.list_id WHERE i.space_id=$1 AND i.list_id=$2 AND i.assignee_id=$3 AND i.status='assigned' AND i.deleted_at IS NULL`, p.SpaceID, p.ListID, p.UserID).Scan(&count, &name); err != nil {
			return err
		}
		body := fmt.Sprintf("Bạn được giao mua %d mặt hàng trong danh sách %s", count, name)
		if count == 0 {
			body = "Phân công mua sắm của bạn đã thay đổi"
		}
		return s.db.Enqueue(ctx, tx, "notification", e.Key+":"+p.BatchID, map[string]string{"userId": p.UserID, "title": "Phân công mua sắm", "body": body, "type": "assignment"}, s.db.Clock.Now())
	})
}
