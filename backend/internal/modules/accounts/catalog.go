package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"dicho/internal/kernel"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

func (s *Service) ValidateFood(ctx context.Context, tx pgx.Tx, sid, foodID, unitID string) (platform.FoodUnit, error) {
	var out platform.FoodUnit
	var factor string
	err := tx.QueryRow(ctx, `SELECT f.id::text,f.name,u.id::text,u.dimension,u.factor_to_base::text FROM catalog.foods f JOIN catalog.units u ON u.id=COALESCE(NULLIF($3,'')::uuid,f.default_unit_id) WHERE f.id=$2 AND (f.scope='global' OR f.owner_space_id=$1) AND f.status IN ('active','pending_review') AND u.active`, sid, foodID, unitID).Scan(&out.FoodID, &out.Name, &out.UnitID, &out.Dimension, &factor)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, platform.NotFound()
	}
	if err != nil {
		return out, err
	}
	out.Factor, err = kernel.ParseQuantity(factor)
	return out, err
}
func (s *Service) CreatePrivateFood(ctx context.Context, tx pgx.Tx, sid, name, category, unit string) (platform.FoodUnit, error) {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n < 1 || n > 100 {
		return platform.FoodUnit{}, platform.Invalid("Tên thực phẩm phải có 1–100 ký tự")
	}
	var active bool
	if err := tx.QueryRow(ctx, "SELECT active FROM catalog.food_categories WHERE id=$1", category).Scan(&active); err != nil {
		return platform.FoodUnit{}, platform.NotFound()
	}
	if !active {
		return platform.FoodUnit{}, platform.Rule("Loại thực phẩm đã ngừng sử dụng")
	}
	id := platform.ID()
	_, err := tx.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id,status) VALUES($1,$2,$3,'space',$4,$5,'pending_review')", id, strings.TrimSpace(name), category, sid, unit)
	if err != nil {
		return platform.FoodUnit{}, err
	}
	return s.ValidateFood(ctx, tx, sid, id, unit)
}
func (s *Service) RegisterCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /it4788/v1/catalog", platform.Handle(s.catalog))
	for _, kind := range []string{"food-categories", "units", "foods"} {
		for _, verb := range []string{"GET", "POST"} {
			mux.HandleFunc(verb+" /it4788/v1/admin/"+kind, platform.Handle(s.catalogAdmin(kind)))
		}
		for _, verb := range []string{"PUT", "DELETE"} {
			mux.HandleFunc(verb+" /it4788/v1/admin/"+kind+"/{id}", platform.Handle(s.catalogAdmin(kind)))
		}
	}
	mux.HandleFunc("POST /it4788/v1/admin/foods/{id}/review", platform.Handle(s.reviewFood))
}
func queryMaps(ctx context.Context, q platform.Querier, sql string, args ...any) ([]map[string]any, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, col := range rows.FieldDescriptions() {
			row[col.Name] = values[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
func (s *Service) catalog(w http.ResponseWriter, r *http.Request) error {
	categories, err := queryMaps(r.Context(), s.db.Pool, "SELECT id::text,name FROM catalog.food_categories WHERE active ORDER BY name")
	if err != nil {
		return err
	}
	units, err := queryMaps(r.Context(), s.db.Pool, `SELECT id::text,name,dimension,factor_to_base::text AS "factorToBase",is_base AS "isBase" FROM catalog.units WHERE active ORDER BY dimension,factor_to_base`)
	if err != nil {
		return err
	}
	for _, u := range units {
		u["factorToBase"] = json.Number(u["factorToBase"].(string))
	}
	tag := `"catalog-1"`
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(304)
		return nil
	}
	platform.Write(w, 200, map[string]any{"categories": categories, "units": units, "locations": []string{"chill", "freezer", "crisper", "door", "pantry_shelf"}})
	return nil
}

type catalogInput struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Dimension       string          `json:"dimension"`
	Factor          kernel.Quantity `json:"factorToBase"`
	IsBase          bool            `json:"isBase"`
	CategoryID      string          `json:"categoryId"`
	UnitID          string          `json:"defaultUnitId"`
	Status          string          `json:"status"`
	Active          *bool           `json:"active"`
	DefaultLocation string          `json:"defaultLocation"`
	ShelfLives      map[string]int  `json:"shelfLives"`
}

func (s *Service) catalogAdmin(kind string) platform.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if err := platform.Admin(r); err != nil {
			return err
		}
		table := map[string]string{"food-categories": "food_categories", "units": "units", "foods": "foods"}[kind]
		if r.Method == "GET" {
			rows, err := queryMaps(r.Context(), s.db.Pool, "SELECT to_jsonb(x) AS item FROM catalog."+table+" x ORDER BY name LIMIT 100")
			if err != nil {
				return err
			}
			items := []any{}
			for _, row := range rows {
				items = append(items, row["item"])
			}
			platform.Write(w, 200, map[string]any{"items": items})
			return nil
		}
		id := r.PathValue("id")
		var in catalogInput
		if r.Method != "DELETE" {
			if err := platform.Decode(r, &in); err != nil {
				return err
			}
			if n := utf8.RuneCountInString(strings.TrimSpace(in.Name)); n < 1 || n > 100 {
				return platform.Invalid("Tên danh mục không hợp lệ")
			}
		}
		if r.Method == "POST" {
			id = in.ID
			if id == "" {
				id = platform.ID()
			}
		}
		err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
			var old json.RawMessage
			if r.Method != "POST" {
				if err := tx.QueryRow(r.Context(), "SELECT to_jsonb(x) FROM catalog."+table+" x WHERE id=$1 FOR UPDATE", id).Scan(&old); err != nil {
					return err
				}
			}
			var err error
			active := true
			if in.Active != nil {
				active = *in.Active
			}
			if r.Method == "DELETE" {
				if kind == "foods" {
					_, err = tx.Exec(r.Context(), "UPDATE catalog.foods SET status='inactive',version=version+1 WHERE id=$1", id)
				} else {
					_, err = tx.Exec(r.Context(), "DELETE FROM catalog."+table+" WHERE id=$1", id)
				}
			} else {
				switch kind {
				case "food-categories":
					if r.Method == "POST" {
						_, err = tx.Exec(r.Context(), "INSERT INTO catalog.food_categories(id,name,active) VALUES($1,$2,$3)", id, strings.TrimSpace(in.Name), active)
					} else {
						_, err = tx.Exec(r.Context(), "UPDATE catalog.food_categories SET name=$2,active=$3 WHERE id=$1", id, strings.TrimSpace(in.Name), active)
					}
				case "units":
					if in.Dimension != "mass" && in.Dimension != "volume" && in.Dimension != "count" {
						return platform.Invalid("Nhóm đại lượng không hợp lệ")
					}
					if in.Factor <= 0 || int64(in.Factor)%100 != 0 || in.IsBase && in.Factor != kernel.Quantity(kernel.Scale) {
						return platform.Invalid("Hệ số đơn vị không hợp lệ")
					}
					if r.Method == "POST" {
						_, err = tx.Exec(r.Context(), "INSERT INTO catalog.units(id,name,dimension,factor_to_base,is_base,active) VALUES($1,$2,$3,$4,$5,$6)", id, in.Name, in.Dimension, in.Factor.String(), in.IsBase, active)
					} else {
						var previous string
						if err = tx.QueryRow(r.Context(), "SELECT dimension FROM catalog.units WHERE id=$1", id).Scan(&previous); err != nil {
							return err
						}
						if previous != in.Dimension {
							var used bool
							if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM catalog.foods WHERE default_unit_id=$1)", id).Scan(&used); err != nil {
								return err
							}
							if used {
								return platform.Rule("Đơn vị đang được tham chiếu")
							}
						}
						_, err = tx.Exec(r.Context(), "UPDATE catalog.units SET name=$2,dimension=$3,factor_to_base=$4,is_base=$5,active=$6 WHERE id=$1", id, in.Name, in.Dimension, in.Factor.String(), in.IsBase, active)
					}
				case "foods":
					if in.CategoryID == "" || in.UnitID == "" {
						return platform.Invalid("Cần loại và đơn vị mặc định")
					}
					if in.Status == "" {
						in.Status = "active"
					}
					if in.Status != "active" && in.Status != "inactive" {
						return platform.Invalid("Trạng thái không hợp lệ")
					}
					if in.DefaultLocation == "" {
						in.DefaultLocation = "chill"
					}
					if r.Method == "POST" {
						_, err = tx.Exec(r.Context(), "INSERT INTO catalog.foods(id,name,category_id,default_unit_id,status,default_location) VALUES($1,$2,$3,$4,$5,$6)", id, in.Name, in.CategoryID, in.UnitID, in.Status, in.DefaultLocation)
					} else {
						_, err = tx.Exec(r.Context(), "UPDATE catalog.foods SET name=$2,category_id=$3,default_unit_id=$4,status=$5,default_location=$6,version=version+1 WHERE id=$1", id, in.Name, in.CategoryID, in.UnitID, in.Status, in.DefaultLocation)
					}
					if err == nil {
						for loc, days := range in.ShelfLives {
							if days < 1 || days > 3650 {
								return platform.Invalid("Hạn dùng không hợp lệ")
							}
							_, err = tx.Exec(r.Context(), "INSERT INTO catalog.food_shelf_lives(food_id,storage_location,days) VALUES($1,$2,$3) ON CONFLICT(food_id,storage_location) DO UPDATE SET days=EXCLUDED.days", id, loc, days)
							if err != nil {
								return err
							}
						}
					}
				}
			}
			if err != nil {
				return err
			}
			return s.audit(r.Context(), tx, platform.Current(r).UserID, "catalog."+r.Method, id, old, in)
		})
		if err != nil {
			return err
		}
		status := 200
		if r.Method == "POST" {
			status = 201
		}
		platform.Write(w, status, map[string]any{"id": id})
		return nil
	}
}
func (s *Service) audit(ctx context.Context, tx pgx.Tx, actor, action, id string, old, next any) error {
	a, err := json.Marshal(old)
	if err != nil {
		return err
	}
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO admin.audit_logs(id,actor_id,action,entity_id,old_value,new_value) VALUES($1,$2,$3,$4,$5,$6)", platform.ID(), actor, action, id, a, b)
	return err
}
func (s *Service) reviewFood(w http.ResponseWriter, r *http.Request) error {
	if err := platform.Admin(r); err != nil {
		return err
	}
	var in struct {
		Action string `json:"action"`
		Target string `json:"targetFoodId"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.Action != "approve" && in.Action != "merge" {
		return platform.Invalid("Thao tác duyệt không hợp lệ")
	}
	id := r.PathValue("id")
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var status, scope string
		var old json.RawMessage
		if err := tx.QueryRow(r.Context(), "SELECT status,scope,to_jsonb(f) FROM catalog.foods f WHERE id=$1 FOR UPDATE", id).Scan(&status, &scope, &old); err != nil {
			return err
		}
		if status != "pending_review" || scope != "space" {
			return platform.Conflict()
		}
		if in.Action == "approve" {
			if _, err := tx.Exec(r.Context(), "UPDATE catalog.foods SET scope='global',owner_space_id=NULL,status='active',version=version+1 WHERE id=$1", id); err != nil {
				return err
			}
		} else {
			if in.Target == id {
				return platform.Invalid("Thực phẩm đích phải khác nguồn")
			}
			var ok bool
			if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM catalog.foods WHERE id=$1 AND scope='global' AND status='active')", in.Target).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return platform.NotFound()
			}
			if _, err := tx.Exec(r.Context(), "UPDATE catalog.foods SET status='inactive',merged_into=$2,version=version+1 WHERE id=$1", id, in.Target); err != nil {
				return err
			}
			if err := s.db.Enqueue(r.Context(), tx, "food.merged", id, map[string]any{"sourceFoodId": id, "targetFoodId": in.Target}, s.db.Clock.Now()); err != nil {
				return err
			}
		}
		return s.audit(r.Context(), tx, platform.Current(r).UserID, "food."+in.Action, id, old, in)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"id": id, "action": in.Action})
	return nil
}
