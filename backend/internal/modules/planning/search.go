package planning

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"sort"
	"unicode/utf8"
)

type SearchResult struct {
	Items  []map[string]any `json:"items"`
	Facets map[string]int   `json:"facets"`
}

func (s *Service) Search(ctx context.Context, user, sid, q string, limit int, foodsOnly bool) (SearchResult, error) {
	out := SearchResult{Items: []map[string]any{}, Facets: map[string]int{}}
	if limit < 1 || limit > 100 || utf8.RuneCountInString(q) > 100 || (foodsOnly && (utf8.RuneCountInString(q) < 2 || limit > 10)) {
		return out, platform.Invalid("Tham số tìm kiếm không hợp lệ")
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.RequireMember(ctx, tx, sid, user); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT jsonb_build_object('id',f.id::text,'name',f.name,'type','food','categoryId',f.category_id::text,'unitId',f.default_unit_id::text,'defaultLocation',f.default_location,'shelfLives',COALESCE((SELECT jsonb_object_agg(l.storage_location,l.days) FROM catalog.food_shelf_lives l WHERE l.food_id=f.id),'{}'::jsonb)) FROM catalog.foods f WHERE f.status IN ('active','pending_review') AND (f.scope='global' OR f.owner_space_id=$1) AND ($2='' OR strpos(replace(unaccent(lower(f.name)),'đ','d'),replace(unaccent(lower($2)),'đ','d'))>0) ORDER BY f.name,f.id LIMIT 100`, sid, q)
		if err != nil {
			return err
		}
		for rows.Next() {
			var b []byte
			if err = rows.Scan(&b); err != nil {
				rows.Close()
				return err
			}
			var item map[string]any
			if err = json.Unmarshal(b, &item); err != nil {
				rows.Close()
				return err
			}
			out.Items = append(out.Items, item)
			out.Facets["food"]++
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		if !foodsOnly {
			rows, err = tx.Query(ctx, `SELECT id::text,name FROM recipe.recipes WHERE deleted_at IS NULL AND status='published' AND (space_id=$1 OR kind='template') AND ($2='' OR strpos(replace(unaccent(lower(name)),'đ','d'),replace(unaccent(lower($2)),'đ','d'))>0) ORDER BY name,id LIMIT 100`, sid, q)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, name string
				if err = rows.Scan(&id, &name); err != nil {
					rows.Close()
					return err
				}
				out.Items = append(out.Items, map[string]any{"id": id, "name": name, "type": "recipe"})
				out.Facets["recipe"]++
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			if lots, ok := s.stock.(platform.LotSearchPort); ok {
				items, err := lots.SearchLots(ctx, tx, sid, q)
				if err != nil {
					return err
				}
				for _, item := range items {
					item["type"] = "lot"
					out.Items = append(out.Items, item)
					out.Facets["lot"]++
				}
			}
		}
		sort.Slice(out.Items, func(i, j int) bool {
			a, b := out.Items[i], out.Items[j]
			if a["name"] != b["name"] {
				return a["name"].(string) < b["name"].(string)
			}
			if a["type"] != b["type"] {
				return a["type"].(string) < b["type"].(string)
			}
			return a["id"].(string) < b["id"].(string)
		})
		if len(out.Items) > limit {
			out.Items = out.Items[:limit]
		}
		return nil
	})
	return out, err
}
