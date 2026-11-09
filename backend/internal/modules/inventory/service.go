package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
	"unicode/utf8"
)

type Service struct {
	db      *platform.DB
	catalog platform.CatalogPort
}

func New(db *platform.DB, catalog platform.CatalogPort) *Service {
	return &Service{db: db, catalog: catalog}
}

type LotInput struct {
	ID              string          `json:"id,omitempty"`
	FoodID          string          `json:"foodId"`
	UnitID          string          `json:"unitId"`
	Quantity        kernel.Quantity `json:"quantity"`
	StorageLocation string          `json:"storageLocation"`
	PurchasedOn     string          `json:"purchasedOn"`
	ExpiresOn       string          `json:"expiresOn"`
	Notes           string          `json:"notes"`
	ConfirmExpired  bool            `json:"confirmExpired"`
	NewFood         *struct {
		Name       string `json:"name"`
		CategoryID string `json:"categoryId"`
	} `json:"newFood,omitempty"`
}
type Lot struct {
	ID              string          `json:"id"`
	FoodID          string          `json:"foodId"`
	Name            string          `json:"name"`
	UnitID          string          `json:"unitId"`
	Dimension       string          `json:"dimension"`
	InitialQty      kernel.Quantity `json:"initialQuantity"`
	RemainingQty    kernel.Quantity `json:"remainingQuantity"`
	StorageLocation string          `json:"storageLocation"`
	PurchasedOn     string          `json:"purchasedOn"`
	ExpiresOn       string          `json:"expiresOn"`
	ExpiryState     string          `json:"expiryState"`
	Lifecycle       string          `json:"lifecycle"`
	Notes           string          `json:"notes"`
	Version         int64           `json:"version"`
	TotalCost       *int64          `json:"totalCost,omitempty"`
}

const lotColumns = `l.id::text,l.food_id::text,f.name,l.unit_id::text,l.dimension,l.initial_qty::text,l.remaining_qty::text,l.storage_location,to_char(l.purchased_on,'YYYY-MM-DD'),to_char(l.expires_on,'YYYY-MM-DD'),l.lifecycle,l.notes,l.version,l.total_cost`

func scanLot(row pgx.Row) (Lot, error) {
	var l Lot
	err := row.Scan(&l.ID, &l.FoodID, &l.Name, &l.UnitID, &l.Dimension, &l.InitialQty, &l.RemainingQty, &l.StorageLocation, &l.PurchasedOn, &l.ExpiresOn, &l.Lifecycle, &l.Notes, &l.Version, &l.TotalCost)
	return l, err
}
func (s *Service) date(value string) (time.Time, error) {
	if value == "" {
		return s.db.Today(), nil
	}
	d, err := time.ParseInLocation("2006-01-02", value, s.db.Today().Location())
	if err != nil {
		return time.Time{}, platform.Invalid("Ngày không hợp lệ")
	}
	return d, nil
}
func (s *Service) expiry(ctx context.Context, q platform.Querier, food, location string, purchased time.Time, value string) (time.Time, error) {
	if value != "" {
		return s.date(value)
	}
	var days int
	err := q.QueryRow(ctx, `SELECT days FROM catalog.food_shelf_lives WHERE food_id=$1 AND storage_location=$2`, food, location).Scan(&days)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, platform.Invalid("Cần nhập ngày hết hạn cho vị trí này")
	}
	return purchased.AddDate(0, 0, days), err
}
func (s *Service) reminderDays(ctx context.Context, q platform.Querier, uid string) int {
	var n int
	err := q.QueryRow(ctx, `SELECT COALESCE((SELECT reminder_days FROM identity.user_settings WHERE user_id=$1),(SELECT (value::text)::int FROM admin.system_configs WHERE key='reminder.defaultDays'),3)`, uid).Scan(&n)
	if err != nil {
		return 3
	}
	return n
}
func (s *Service) CreateLot(ctx context.Context, sid, uid string, c LotInput) (Lot, error) {
	var out Lot
	if err := validQuantity(c.Quantity); err != nil {
		return out, err
	}
	if !validLocation(c.StorageLocation) || utf8.RuneCountInString(c.Notes) > 200 {
		return out, platform.Invalid("Vị trí hoặc ghi chú không hợp lệ")
	}
	purchased, err := s.date(c.PurchasedOn)
	if err != nil {
		return out, err
	}
	if purchased.After(s.db.Today()) {
		return out, platform.Invalid("Ngày mua không được sau hôm nay")
	}
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, uid); err != nil {
			return err
		}
		if c.FoodID == "" && c.NewFood != nil {
			creator, ok := s.catalog.(platform.CatalogCreator)
			if !ok {
				return platform.Rule("Danh mục không hỗ trợ thêm thực phẩm")
			}
			food, err := creator.CreatePrivateFood(ctx, tx, sid, c.NewFood.Name, c.NewFood.CategoryID, c.UnitID)
			if err != nil {
				return err
			}
			c.FoodID = food.FoodID
		}
		expiry, err := s.expiry(ctx, tx, c.FoodID, c.StorageLocation, purchased, c.ExpiresOn)
		if err != nil {
			return err
		}
		if expiry.Before(purchased) {
			return platform.Invalid("Ngày hết hạn không được trước ngày mua")
		}
		if expiry.Before(s.db.Today()) && !c.ConfirmExpired {
			return platform.Rule("Cần xác nhận thực phẩm đã hết hạn")
		}
		ids, err := s.AddLots(ctx, tx, sid, uid, []platform.StockInput{{FoodID: c.FoodID, UnitID: c.UnitID, Quantity: c.Quantity, Location: c.StorageLocation, PurchasedOn: purchased, ExpiresOn: expiry}})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE pantry.food_lots SET notes=$2 WHERE id=$1", ids[0], c.Notes); err != nil {
			return err
		}
		out, err = scanLot(tx.QueryRow(ctx, `SELECT `+lotColumns+` FROM pantry.food_lots l JOIN catalog.foods f ON f.id=l.food_id WHERE l.id=$1 AND l.space_id=$2`, ids[0], sid))
		if err != nil {
			return err
		}
		out.ExpiryState = ExpiryState(expiry, s.db.Today(), s.reminderDays(ctx, tx, uid))
		return s.db.Activity(ctx, tx, sid, uid, "lot.created", out.ID, out)
	})
	return out, err
}

func (s *Service) AddLots(ctx context.Context, tx pgx.Tx, sid, uid string, inputs []platform.StockInput) ([]string, error) {
	ids := make([]string, 0, len(inputs))
	for _, in := range inputs {
		if err := validQuantity(in.Quantity); err != nil {
			return nil, err
		}
		if !validLocation(in.Location) {
			return nil, platform.Invalid("Vị trí không hợp lệ")
		}
		purchased := in.PurchasedOn
		if purchased.IsZero() {
			purchased = s.db.Today()
		}
		if purchased.After(s.db.Today()) || in.ExpiresOn.Before(purchased) {
			return nil, platform.Invalid("Ngày mua hoặc hết hạn không hợp lệ")
		}
		food, err := s.catalog.ValidateFood(ctx, tx, sid, in.FoodID, in.UnitID)
		if err != nil {
			return nil, err
		}
		base, err := in.Quantity.Mul(food.Factor)
		if err != nil {
			return nil, platform.Invalid("Không thể quy đổi chính xác số lượng")
		}
		id := platform.ID()
		var source any
		if in.ItemID != "" {
			source = in.ItemID
		}
		_, err = tx.Exec(ctx, `INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location,total_cost,source_item_id) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$10,$11,$12,$13)`, id, sid, in.FoodID, in.UnitID, food.Factor.String(), food.Dimension, in.Quantity.String(), base.String(), purchased, in.ExpiresOn, in.Location, in.TotalCost, source)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,actor_id,occurred_at) VALUES($1,$2,$3,$4,'stock_in',$5,$6,$7)`, platform.ID(), sid, id, in.FoodID, base.String(), uid, s.db.Clock.Now()); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) Available(ctx context.Context, tx pgx.Tx, sid string, on time.Time) ([]platform.Stock, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,food_id::text,unit_id::text,dimension,base_remaining::text,expires_on,version FROM pantry.food_lots WHERE space_id=$1 AND lifecycle='active' AND deleted_at IS NULL AND expires_on>=$2 ORDER BY food_id,expires_on,id`, sid, on.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]platform.Stock, 0)
	for rows.Next() {
		var stock platform.Stock
		if err := rows.Scan(&stock.LotID, &stock.FoodID, &stock.UnitID, &stock.Dimension, &stock.Quantity, &stock.ExpiresOn, &stock.Version); err != nil {
			return nil, err
		}
		out = append(out, stock)
	}
	return out, rows.Err()
}

func (s *Service) Consume(ctx context.Context, tx pgx.Tx, sid, mealID string, lines []platform.Consumption) error {
	sorted := append([]platform.Consumption(nil), lines...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].FoodID == sorted[j].FoodID {
			return sorted[i].LotID < sorted[j].LotID
		}
		return sorted[i].FoodID < sorted[j].FoodID
	})
	if mealID != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pantry.stock_movements WHERE space_id=$1 AND meal_id=$2 AND kind='consume')`, sid, mealID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
	}
	for _, line := range sorted {
		if line.Quantity <= 0 {
			return platform.Invalid("Số lượng phải dương")
		}
		if line.LotID != "" && !line.AllowExpired {
			var expired bool
			if err := tx.QueryRow(ctx, `SELECT expires_on<$3::date FROM pantry.food_lots WHERE id=$1 AND space_id=$2`, line.LotID, sid, s.db.Today().Format("2006-01-02")).Scan(&expired); err != nil {
				return err
			}
			if expired {
				return platform.Rule("Cần xác nhận sử dụng lô đã hết hạn")
			}
		}
		rows, err := tx.Query(ctx, `SELECT id::text,expires_on,base_remaining::text,unit_factor::text FROM pantry.food_lots WHERE space_id=$1 AND food_id=$2 AND lifecycle='active' AND deleted_at IS NULL AND (($3::text='' AND expires_on>=$4::date) OR id::text=$3) ORDER BY expires_on,id FOR UPDATE`, sid, line.FoodID, line.LotID, s.db.Today().Format("2006-01-02"))
		if err != nil {
			return err
		}
		lots := make([]AllocationLot, 0)
		factors := map[string]kernel.Quantity{}
		remaining := map[string]kernel.Quantity{}
		for rows.Next() {
			var lot AllocationLot
			var factor kernel.Quantity
			if err = rows.Scan(&lot.ID, &lot.ExpiresOn, &lot.Available, &factor); err != nil {
				rows.Close()
				return err
			}
			lots = append(lots, lot)
			factors[lot.ID] = factor
			remaining[lot.ID] = lot.Available
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		allocations, err := Allocate(lots, line.Quantity, s.db.Today(), line.LotID)
		if err != nil {
			return err
		}
		for _, a := range allocations {
			base := remaining[a.LotID] - a.Quantity
			display, err := base.Div(factors[a.LotID])
			if err != nil || !display.TwoDecimals() {
				return platform.Rule("Đơn vị lô không biểu diễn chính xác lượng còn lại")
			}
			life := "active"
			if display == 0 {
				life = "consumed"
			}
			if _, err = tx.Exec(ctx, `UPDATE pantry.food_lots SET remaining_qty=$3,base_remaining=$4,lifecycle=$5,version=version+1 WHERE id=$1 AND space_id=$2`, a.LotID, sid, display.String(), base.String(), life); err != nil {
				return err
			}
			var meal any
			if mealID != "" {
				meal = mealID
			}
			if _, err = tx.Exec(ctx, `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,meal_id,occurred_at) VALUES($1,$2,$3,$4,'consume',$5,$6,$7)`, platform.ID(), sid, a.LotID, line.FoodID, a.Quantity.String(), meal, s.db.Clock.Now()); err != nil {
				return err
			}
		}
	}
	return nil
}
