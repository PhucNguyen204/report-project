package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type ListInput struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	PlannedDate string `json:"plannedDate"`
	Notes       string `json:"notes"`
}
type List struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PlannedDate string `json:"plannedDate"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	Version     int64  `json:"version"`
	TotalCost   int64  `json:"totalCost"`
}
type ItemInput struct {
	ID       string          `json:"id,omitempty"`
	FoodID   string          `json:"foodId"`
	Name     string          `json:"name"`
	UnitID   string          `json:"unitId"`
	Quantity kernel.Quantity `json:"quantity"`
	Notes    string          `json:"notes"`
}
type Item struct {
	ID             string           `json:"id"`
	ListID         string           `json:"listId"`
	FoodID         *string          `json:"foodId"`
	Name           string           `json:"name"`
	UnitID         string           `json:"unitId"`
	Dimension      string           `json:"dimension"`
	Quantity       kernel.Quantity  `json:"quantity"`
	ActualQuantity *kernel.Quantity `json:"actualQuantity"`
	UnitPrice      *int64           `json:"unitPrice"`
	Status         string           `json:"status"`
	AssigneeID     *string          `json:"assigneeId"`
	PurchasedBy    *string          `json:"purchasedBy"`
	PurchasedAt    *time.Time       `json:"purchasedAt"`
	LotID          *string          `json:"lotId"`
	Notes          string           `json:"notes"`
	Version        int64            `json:"version"`
	UnitFactor     kernel.Quantity  `json:"-"`
}

const itemColumns = `i.id::text,i.list_id::text,i.food_id::text,i.name,i.unit_id::text,i.dimension,i.quantity::text,i.actual_quantity::text,i.unit_price,i.status,i.assignee_id::text,i.purchased_by::text,i.purchased_at,i.lot_id::text,i.notes,i.version,i.unit_factor::text`

func scanItem(row pgx.Row) (Item, error) {
	var i Item
	var actual *string
	err := row.Scan(&i.ID, &i.ListID, &i.FoodID, &i.Name, &i.UnitID, &i.Dimension, &i.Quantity, &actual, &i.UnitPrice, &i.Status, &i.AssigneeID, &i.PurchasedBy, &i.PurchasedAt, &i.LotID, &i.Notes, &i.Version, &i.UnitFactor)
	if err != nil {
		return i, err
	}
	if actual != nil {
		q, err := kernel.ParseQuantity(*actual)
		if err != nil {
			return i, err
		}
		i.ActualQuantity = &q
	}
	return i, nil
}
func scanList(row pgx.Row) (List, error) {
	var l List
	err := row.Scan(&l.ID, &l.Name, &l.PlannedDate, &l.Status, &l.Notes, &l.Version)
	return l, err
}

const listColumns = `id::text,name,to_char(planned_date,'YYYY-MM-DD'),status,notes,version`

func (s *Service) CreateList(ctx context.Context, sid, uid string, c ListInput) (List, error) {
	var out List
	if c.ID != "" && !platform.UUID7(c.ID) {
		return out, platform.Invalid("id phải là UUIDv7")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(c.Name)); n < 1 || n > 100 || utf8.RuneCountInString(c.Notes) > 200 {
		return out, platform.Invalid("Tên hoặc ghi chú không hợp lệ")
	}
	date, err := s.date(c.PlannedDate)
	if err != nil {
		return out, err
	}
	if date.Before(s.db.Today()) {
		return out, platform.Invalid("Ngày mua không được ở quá khứ")
	}
	if c.ID == "" {
		c.ID = platform.ID()
	}
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, uid); err != nil {
			return err
		}
		var err error
		out, err = scanList(tx.QueryRow(ctx, `INSERT INTO shopping.shopping_lists(id,space_id,name,planned_date,notes) VALUES($1,$2,$3,$4,$5) RETURNING `+listColumns, c.ID, sid, c.Name, date, c.Notes))
		if err != nil {
			return err
		}
		return s.db.Activity(ctx, tx, sid, uid, "shopping.list.created", out.ID, out)
	})
	return out, err
}
func (s *Service) lockList(ctx context.Context, tx pgx.Tx, sid, id string, allowCompleted bool) (List, error) {
	l, err := scanList(tx.QueryRow(ctx, `SELECT `+listColumns+` FROM shopping.shopping_lists WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, sid))
	if err != nil {
		return l, err
	}
	if l.Status == "completed" && !allowCompleted {
		return l, platform.Rule("BR08: Danh sách đã hoàn tất")
	}
	return l, nil
}
func (s *Service) AddItem(ctx context.Context, sid, uid, listID string, c ItemInput) (Item, error) {
	var out Item
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, uid); err != nil {
			return err
		}
		if _, err := s.lockList(ctx, tx, sid, listID, false); err != nil {
			return err
		}
		var err error
		out, err = s.addItem(ctx, tx, sid, listID, c)
		if err != nil {
			return err
		}
		if err = s.recalculate(ctx, tx, sid, listID); err != nil {
			return err
		}
		return s.db.Activity(ctx, tx, sid, uid, "shopping.item.created", out.ID, out)
	})
	return out, err
}
func (s *Service) addItem(ctx context.Context, tx pgx.Tx, sid, listID string, c ItemInput) (Item, error) {
	var out Item
	if c.ID != "" {
		if !platform.UUID7(c.ID) {
			return out, platform.Invalid("id phải là UUIDv7")
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shopping.shopping_items WHERE id=$1)", c.ID).Scan(&exists); err != nil {
			return out, err
		}
		if exists {
			return out, platform.Err(409, 4003, "Dữ liệu đã tồn tại")
		}
	}
	if err := validQuantity(c.Quantity); err != nil {
		return out, err
	}
	if utf8.RuneCountInString(c.Notes) > 200 {
		return out, platform.Invalid("Ghi chú quá dài")
	}
	var food any
	var factor kernel.Quantity
	var dimension string
	if c.FoodID != "" {
		f, err := s.catalog.ValidateFood(ctx, tx, sid, c.FoodID, c.UnitID)
		if err != nil {
			return out, err
		}
		c.FoodID = f.FoodID
		c.UnitID = f.UnitID
		food = c.FoodID
		c.Name = f.Name
		factor = f.Factor
		dimension = f.Dimension
		existing, err := scanItem(tx.QueryRow(ctx, `SELECT `+itemColumns+` FROM shopping.shopping_items i WHERE space_id=$1 AND list_id=$2 AND food_id=$3 AND dimension=$4 AND status IN ('to_buy','assigned') AND deleted_at IS NULL ORDER BY id LIMIT 1 FOR UPDATE`, sid, listID, c.FoodID, dimension))
		if err == nil {
			base, err := c.Quantity.Mul(factor)
			if err != nil {
				return out, platform.Invalid("Không thể quy đổi chính xác")
			}
			converted, err := base.Div(existing.UnitFactor)
			merged := existing.Quantity + converted
			unitID, unitFactor := existing.UnitID, existing.UnitFactor
			if err != nil || !converted.TwoDecimals() {
				oldBase, e := existing.Quantity.Mul(existing.UnitFactor)
				if e != nil {
					return out, e
				}
				merged = oldBase + base
				if merged < oldBase || validQuantity(merged) != nil {
					return out, platform.Rule("Đơn vị cơ sở không biểu diễn chính xác lượng gộp")
				}
				if existing.UnitPrice != nil {
					return out, platform.Rule("Cần giữ nguyên đơn vị khi đã nhập giá")
				}
				if e = tx.QueryRow(ctx, `SELECT id::text FROM catalog.units WHERE dimension=$1 AND is_base AND active`, dimension).Scan(&unitID); e != nil {
					return out, e
				}
				unitFactor = kernel.Quantity(kernel.Scale)
			}
			if merged < existing.Quantity && unitID == existing.UnitID || validQuantity(merged) != nil {
				return out, platform.Invalid("Số lượng vượt giới hạn")
			}
			return scanItem(tx.QueryRow(ctx, `UPDATE shopping.shopping_items i SET quantity=$3,unit_id=$4,unit_factor=$5,version=version+1 WHERE id=$1 AND space_id=$2 RETURNING `+itemColumns, existing.ID, sid, merged.String(), unitID, unitFactor.String()))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
	} else {
		if n := utf8.RuneCountInString(strings.TrimSpace(c.Name)); n < 1 || n > 100 {
			return out, platform.Invalid("Tên mặt hàng không hợp lệ")
		}
		if err := tx.QueryRow(ctx, `SELECT dimension,factor_to_base::text FROM catalog.units WHERE id=$1 AND active`, c.UnitID).Scan(&dimension, &factor); err != nil {
			return out, err
		}
	}
	if c.ID == "" {
		c.ID = platform.ID()
	}
	return scanItem(tx.QueryRow(ctx, `INSERT INTO shopping.shopping_items AS i(id,space_id,list_id,food_id,name,unit_id,unit_factor,dimension,quantity,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+itemColumns, c.ID, sid, listID, food, c.Name, c.UnitID, factor.String(), dimension, c.Quantity.String(), c.Notes))
}
func (s *Service) recalculate(ctx context.Context, tx pgx.Tx, sid, listID string) error {
	_, err := tx.Exec(ctx, `UPDATE shopping.shopping_lists l SET status=CASE WHEN NOT EXISTS(SELECT 1 FROM shopping.shopping_items i WHERE i.list_id=l.id AND i.deleted_at IS NULL) THEN 'draft' WHEN NOT EXISTS(SELECT 1 FROM shopping.shopping_items i WHERE i.list_id=l.id AND i.deleted_at IS NULL AND i.status IN ('to_buy','assigned')) THEN 'completed' WHEN EXISTS(SELECT 1 FROM shopping.shopping_items i WHERE i.list_id=l.id AND i.deleted_at IS NULL AND i.status IN ('purchased','stocked')) THEN 'shopping' ELSE 'draft' END,version=version+1 WHERE l.id=$1 AND l.space_id=$2`, listID, sid)
	return err
}

type PurchaseInput struct {
	ItemID         string           `json:"itemId"`
	ActualQuantity *kernel.Quantity `json:"actualQuantity,omitempty"`
	UnitPrice      *int64           `json:"unitPrice,omitempty"`
	OnBehalf       bool             `json:"onBehalf"`
	PurchasedAt    *time.Time       `json:"purchasedAt,omitempty"`
}
type PurchaseResult struct {
	ItemID  string `json:"itemId"`
	Outcome string `json:"outcome"`
	Current Item   `json:"current"`
}

func (s *Service) lockItems(ctx context.Context, tx pgx.Tx, sid string, ids []string) (map[string]Item, error) {
	if len(ids) < 1 || len(ids) > 50 {
		return nil, platform.Invalid("Cần từ 1 đến 50 mặt hàng")
	}
	seen := map[string]bool{}
	listIDs := []string{}
	for _, id := range ids {
		if seen[id] {
			return nil, platform.Invalid("Mặt hàng bị lặp trong yêu cầu")
		}
		seen[id] = true
		var listID string
		if err := tx.QueryRow(ctx, `SELECT list_id::text FROM shopping.shopping_items WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL`, id, sid).Scan(&listID); err != nil {
			return nil, err
		}
		listIDs = append(listIDs, listID)
	}
	sort.Strings(listIDs)
	last := ""
	for _, id := range listIDs {
		if id != last {
			if _, err := s.lockList(ctx, tx, sid, id, true); err != nil {
				return nil, err
			}
			last = id
		}
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	out := map[string]Item{}
	for _, id := range sorted {
		item, err := scanItem(tx.QueryRow(ctx, `SELECT `+itemColumns+` FROM shopping.shopping_items i WHERE id=$1 AND space_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, sid))
		if err != nil {
			return nil, err
		}
		out[id] = item
	}
	return out, nil
}
func (s *Service) Purchase(ctx context.Context, sid, uid string, inputs []PurchaseInput) ([]PurchaseResult, error) {
	results := make([]PurchaseResult, 0, len(inputs))
	ids := []string{}
	for _, in := range inputs {
		ids = append(ids, in.ItemID)
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		results = results[:0]
		if _, err := s.db.LockMember(ctx, tx, sid, uid); err != nil {
			return err
		}
		items, err := s.lockItems(ctx, tx, sid, ids)
		if err != nil {
			return err
		}
		changed := 0
		lists := map[string]bool{}
		for _, in := range inputs {
			item := items[in.ItemID]
			if item.Status == "purchased" || item.Status == "stocked" {
				results = append(results, PurchaseResult{item.ID, "already_purchased", item})
				continue
			}
			if item.Status != "to_buy" && item.Status != "assigned" {
				return platform.Rule("BR10: Không thể đánh dấu mặt hàng này")
			}
			if item.AssigneeID != nil && *item.AssigneeID != uid && !in.OnBehalf {
				return platform.Rule("BR10: Cần xác nhận mua thay người được giao")
			}
			amount := item.Quantity
			if in.ActualQuantity != nil {
				amount = *in.ActualQuantity
			}
			if err := validQuantity(amount); err != nil {
				return err
			}
			if in.UnitPrice != nil && *in.UnitPrice < 0 {
				return platform.Invalid("Đơn giá không hợp lệ")
			}
			purchased := s.db.Clock.Now()
			if in.PurchasedAt != nil {
				purchased = *in.PurchasedAt
				if purchased.After(s.db.Clock.Now()) {
					return platform.Invalid("Thời điểm mua không hợp lệ")
				}
			}
			item, err = scanItem(tx.QueryRow(ctx, `UPDATE shopping.shopping_items i SET status='purchased',actual_quantity=$3,unit_price=$4,purchased_by=$5,purchased_at=$6,version=version+1 WHERE id=$1 AND space_id=$2 RETURNING `+itemColumns, item.ID, sid, amount.String(), in.UnitPrice, uid, purchased))
			if err != nil {
				return err
			}
			results = append(results, PurchaseResult{item.ID, "purchased", item})
			changed++
			lists[item.ListID] = true
		}
		if changed == 0 {
			return platform.Conflict()
		}
		for id := range lists {
			if err := s.recalculate(ctx, tx, sid, id); err != nil {
				return err
			}
		}
		return s.db.Activity(ctx, tx, sid, uid, "shopping.purchased", ids[0], results)
	})
	return results, err
}

type StockItemInput struct {
	ItemID          string           `json:"itemId"`
	FoodID          string           `json:"foodId,omitempty"`
	StorageLocation string           `json:"storageLocation"`
	Quantity        *kernel.Quantity `json:"quantity,omitempty"`
	UnitID          string           `json:"unitId,omitempty"`
	PurchasedOn     string           `json:"purchasedOn,omitempty"`
	ExpiresOn       string           `json:"expiresOn,omitempty"`
}
type StockResult struct {
	ItemID      string `json:"itemId"`
	Outcome     string `json:"outcome"`
	LotID       string `json:"lotId"`
	ExpiresOn   string `json:"expiresOn"`
	ExpiryState string `json:"expiryState"`
}

func (s *Service) StockItems(ctx context.Context, sid, uid string, inputs []StockItemInput) ([]StockResult, error) {
	ids := []string{}
	for _, in := range inputs {
		ids = append(ids, in.ItemID)
	}
	result := make([]StockResult, 0, len(inputs))
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		result = result[:0]
		if _, err := s.db.LockMember(ctx, tx, sid, uid); err != nil {
			return err
		}
		items, err := s.lockItems(ctx, tx, sid, ids)
		if err != nil {
			return err
		}
		n := s.reminderDays(ctx, tx, uid)
		for _, in := range inputs {
			item := items[in.ItemID]
			if item.Status == "stocked" && item.LotID != nil {
				var expiry string
				if err := tx.QueryRow(ctx, "SELECT to_char(expires_on,'YYYY-MM-DD') FROM pantry.food_lots WHERE id=$1 AND space_id=$2", *item.LotID, sid).Scan(&expiry); err != nil {
					return err
				}
				d, err := s.date(expiry)
				if err != nil {
					return err
				}
				result = append(result, StockResult{item.ID, "already_stocked", *item.LotID, expiry, ExpiryState(d, s.db.Today(), n)})
				continue
			}
			if item.Status != "purchased" {
				return platform.Conflict()
			}
			food := in.FoodID
			if food == "" && item.FoodID != nil {
				food = *item.FoodID
			}
			if food == "" {
				return platform.Rule("Mặt hàng tên tự do cần chọn thực phẩm")
			}
			amount := item.Quantity
			if item.ActualQuantity != nil {
				amount = *item.ActualQuantity
			}
			if in.Quantity != nil {
				amount = *in.Quantity
			}
			unit := in.UnitID
			if unit == "" {
				unit = item.UnitID
			}
			purchaseDate := s.db.Today()
			if item.PurchasedAt != nil {
				local := item.PurchasedAt.In(s.db.Today().Location())
				purchaseDate = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
			}
			if in.PurchasedOn != "" {
				purchaseDate, err = s.date(in.PurchasedOn)
				if err != nil {
					return err
				}
			}
			canonical, err := s.catalog.ValidateFood(ctx, tx, sid, food, unit)
			if err != nil {
				return err
			}
			food, unit = canonical.FoodID, canonical.UnitID
			expiry, err := s.expiry(ctx, tx, food, in.StorageLocation, purchaseDate, in.ExpiresOn)
			if err != nil {
				return err
			}
			var cost *int64
			if item.UnitPrice != nil {
				actual := item.Quantity
				if item.ActualQuantity != nil {
					actual = *item.ActualQuantity
				}
				value, err := totalCost(actual, *item.UnitPrice)
				if err != nil {
					return err
				}
				cost = &value
			}
			lots, err := s.AddLots(ctx, tx, sid, uid, []platform.StockInput{{ItemID: item.ID, FoodID: food, UnitID: unit, Quantity: amount, Location: in.StorageLocation, PurchasedOn: purchaseDate, ExpiresOn: expiry, TotalCost: cost}})
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE shopping.shopping_items SET status='stocked',lot_id=$3,food_id=$4,version=version+1 WHERE id=$1 AND space_id=$2`, item.ID, sid, lots[0], food); err != nil {
				return err
			}
			result = append(result, StockResult{item.ID, "stocked", lots[0], expiry.Format("2006-01-02"), ExpiryState(expiry, s.db.Today(), n)})
		}
		return s.db.Activity(ctx, tx, sid, uid, "shopping.stocked", ids[0], result)
	})
	return result, err
}

func (s *Service) Compose(ctx context.Context, tx pgx.Tx, sid, uid, listID string, lines []platform.MissingLine) (string, error) {
	return s.ComposePlanned(ctx, tx, sid, uid, platform.Composition{TargetListID: listID, Lines: lines})
}

func (s *Service) ComposePlanned(ctx context.Context, tx pgx.Tx, sid, uid string, c platform.Composition) (string, error) {
	if _, err := s.db.LockMember(ctx, tx, sid, uid); err != nil {
		return "", err
	}
	if len(c.Lines) == 0 || len(c.Lines) > 100 {
		return "", platform.Invalid("Cần chọn từ 1 đến 100 nguyên liệu")
	}
	listID := c.TargetListID
	if listID == "" {
		date := c.PlannedDate
		if date.IsZero() {
			date = s.db.Today()
		}
		date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, s.db.Today().Location())
		if date.Before(s.db.Today()) {
			return "", platform.Invalid("Ngày mua không được ở quá khứ")
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = "Nguyên liệu còn thiếu"
		}
		if utf8.RuneCountInString(name) > 100 {
			return "", platform.Invalid("Tên danh sách quá dài")
		}
		listID = platform.ID()
		if _, err := tx.Exec(ctx, `INSERT INTO shopping.shopping_lists(id,space_id,name,planned_date) VALUES($1,$2,$3,$4)`, listID, sid, name, date); err != nil {
			return "", err
		}
	}
	if _, err := s.lockList(ctx, tx, sid, listID, false); err != nil {
		return "", err
	}
	for _, line := range c.Lines {
		if _, err := s.addItem(ctx, tx, sid, listID, ItemInput{FoodID: line.FoodID, UnitID: line.UnitID, Name: line.Name, Quantity: line.Quantity}); err != nil {
			return "", err
		}
	}
	if err := s.recalculate(ctx, tx, sid, listID); err != nil {
		return "", err
	}
	return listID, nil
}

func (s *Service) Pending(ctx context.Context, tx pgx.Tx, sid string, from, to time.Time) ([]platform.PendingSupply, error) {
	rows, err := tx.Query(ctx, `SELECT i.food_id::text,i.unit_id::text,i.dimension,(COALESCE(i.actual_quantity,i.quantity)*i.unit_factor)::text,CASE WHEN i.status='purchased' THEN (i.purchased_at AT TIME ZONE 'Asia/Ho_Chi_Minh')::date ELSE l.planned_date END FROM shopping.shopping_items i JOIN shopping.shopping_lists l ON l.id=i.list_id WHERE i.space_id=$1 AND i.food_id IS NOT NULL AND i.status IN ('to_buy','assigned','purchased') AND i.deleted_at IS NULL AND l.deleted_at IS NULL AND (i.status='purchased' OR l.planned_date<=$2) ORDER BY i.food_id,i.id`, sid, to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]platform.PendingSupply, 0)
	for rows.Next() {
		var supply platform.PendingSupply
		if err := rows.Scan(&supply.FoodID, &supply.UnitID, &supply.Dimension, &supply.Quantity, &supply.AvailableOn); err != nil {
			return nil, err
		}
		out = append(out, supply)
	}
	return out, rows.Err()
}
func (s *Service) OnMemberLeave(ctx context.Context, tx pgx.Tx, sid, uid string) error {
	_, err := tx.Exec(ctx, `UPDATE shopping.shopping_items SET assignee_id=NULL,status='to_buy',version=version+1 WHERE space_id=$1 AND assignee_id=$2 AND status='assigned' AND deleted_at IS NULL`, sid, uid)
	return err
}
