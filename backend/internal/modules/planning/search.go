package planning

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"regexp"
	"sort"
	"unicode/utf8"
)

type SearchResult struct {
	Items        []map[string]any          `json:"items"`
	Facets       map[string]int            `json:"facets"`
	FilterFacets map[string]map[string]int `json:"filterFacets,omitempty"`
	Next         *platform.SearchKey       `json:"-"`
}
type SearchInput struct {
	Query                                string
	CategoryIDs, Locations, ExpiryStates []string
	After                                platform.SearchKey
	Limit                                int
	FoodsOnly                            bool
}

var searchUUID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func validateSearch(in SearchInput) error {
	if in.Limit < 1 || in.Limit > 100 || !utf8.ValidString(in.Query) || utf8.RuneCountInString(in.Query) > 100 {
		return platform.Invalid("Tham số tìm kiếm không hợp lệ")
	}
	if in.FoodsOnly && (utf8.RuneCountInString(in.Query) < 2 || in.Limit > 10 || in.After.ID != "" || len(in.CategoryIDs)+len(in.Locations)+len(in.ExpiryStates) > 0) {
		return platform.Invalid("Tham số gợi ý tên không hợp lệ")
	}
	for _, id := range in.CategoryIDs {
		if !searchUUID.MatchString(id) {
			return platform.Invalid("Loại thực phẩm không hợp lệ")
		}
	}
	for _, v := range in.Locations {
		if v != "chill" && v != "freezer" && v != "crisper" && v != "door" && v != "pantry_shelf" {
			return platform.Invalid("Vị trí không hợp lệ")
		}
	}
	for _, v := range in.ExpiryStates {
		if v != "fresh" && v != "expiring" && v != "expired" {
			return platform.Invalid("Trạng thái hạn dùng không hợp lệ")
		}
	}
	if in.After.ID != "" && (!searchUUID.MatchString(in.After.ID) || (in.After.Type != "food" && in.After.Type != "recipe" && in.After.Type != "lot") || utf8.RuneCountInString(in.After.Name) > 100) {
		return platform.Invalid("Con trỏ tìm kiếm không hợp lệ")
	}
	return nil
}
func (s *Service) Search(ctx context.Context, user, sid, q string, limit int, foodsOnly bool) (SearchResult, error) {
	return s.SearchPage(ctx, user, sid, SearchInput{Query: q, Limit: limit, FoodsOnly: foodsOnly})
}
func (s *Service) SearchPage(ctx context.Context, user, sid string, in SearchInput) (SearchResult, error) {
	out := SearchResult{Items: []map[string]any{}, Facets: map[string]int{"food": 0}}
	if !in.FoodsOnly {
		out.Facets["recipe"] = 0
		out.Facets["lot"] = 0
		out.FilterFacets = map[string]map[string]int{"categoryId": {}, "location": {}, "expiryState": {}}
	}
	if err := validateSearch(in); err != nil {
		return out, err
	}
	take := in.Limit + 1
	if in.FoodsOnly {
		take = in.Limit
	}
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		if len(in.Locations)+len(in.ExpiryStates) == 0 {
			items, total, categories, err := s.searchFoods(ctx, tx, sid, in, take)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, items...)
			out.Facets["food"] = total
			if !in.FoodsOnly {
				for key, n := range categories {
					out.FilterFacets["categoryId"][key] += n
				}
			}
		}
		if in.FoodsOnly {
			return nil
		}
		if len(in.CategoryIDs)+len(in.Locations)+len(in.ExpiryStates) == 0 {
			items, total, err := s.searchRecipes(ctx, tx, sid, in, take)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, items...)
			out.Facets["recipe"] = total
		}
		if s.stock != nil {
			lots, ok := s.stock.(platform.LotSearchPagePort)
			if !ok {
				return platform.Err(503, 5003, "Dịch vụ tìm kiếm tồn kho chưa sẵn sàng")
			}
			page, err := lots.SearchLotsPage(ctx, tx, sid, user, platform.LotSearchQuery{Query: in.Query, CategoryIDs: in.CategoryIDs, Locations: in.Locations, ExpiryStates: in.ExpiryStates, After: in.After, Limit: take})
			if err != nil {
				return err
			}
			out.Items = append(out.Items, page.Items...)
			out.Facets["lot"] = page.Total
			for _, pair := range []struct {
				kind   string
				counts map[string]int
			}{{"categoryId", page.CategoryCounts}, {"location", page.LocationCounts}, {"expiryState", page.ExpiryCounts}} {
				for key, n := range pair.counts {
					out.FilterFacets[pair.kind][key] += n
				}
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		for _, key := range []string{"name", "type", "id"} {
			x, y := a[key].(string), b[key].(string)
			if x != y {
				return x < y
			}
		}
		return false
	})
	if len(out.Items) > in.Limit {
		last := out.Items[in.Limit-1]
		out.Next = &platform.SearchKey{Name: last["name"].(string), Type: last["type"].(string), ID: last["id"].(string)}
		out.Items = out.Items[:in.Limit]
	}
	return out, nil
}
func (s *Service) searchFoods(ctx context.Context, tx pgx.Tx, sid string, in SearchInput, take int) ([]map[string]any, int, map[string]int, error) {
	items := []map[string]any{}
	categories := map[string]int{}
	var raw, facet []byte
	var total int
	err := tx.QueryRow(ctx, `WITH matches AS MATERIALIZED (
 SELECT f.id,f.name,f.category_id,f.default_unit_id,f.default_location FROM catalog.foods f WHERE f.status IN ('active','pending_review') AND (f.scope='global' OR f.owner_space_id=$1)
 AND ($2='' OR strpos(replace(unaccent(lower(f.name)),'đ','d'),replace(unaccent(lower($2)),'đ','d'))>0)
 AND (COALESCE(cardinality($3::uuid[]),0)=0 OR f.category_id=ANY($3))
 ),page AS (SELECT * FROM matches WHERE ($6='' OR (name COLLATE "C",'food'::text COLLATE "C",id)>($4 COLLATE "C",$5 COLLATE "C",NULLIF($6,'')::uuid)) ORDER BY name COLLATE "C",id LIMIT $7)
 SELECT COALESCE((SELECT jsonb_agg(jsonb_build_object('id',p.id::text,'name',p.name,'type','food','categoryId',p.category_id::text,'unitId',p.default_unit_id::text,'defaultLocation',p.default_location,'shelfLives',COALESCE((SELECT jsonb_object_agg(storage_location,days) FROM catalog.food_shelf_lives WHERE food_id=p.id),'{}'::jsonb)) ORDER BY p.name COLLATE "C",p.id) FROM page p),'[]'::jsonb),
 (SELECT count(*) FROM matches),COALESCE((SELECT jsonb_object_agg(category,n) FROM (SELECT category_id::text AS category,count(*) AS n FROM matches GROUP BY category_id)x),'{}'::jsonb)`, sid, in.Query, in.CategoryIDs, in.After.Name, in.After.Type, in.After.ID, take).Scan(&raw, &total, &facet)
	if err != nil {
		return nil, 0, nil, err
	}
	if err = json.Unmarshal(raw, &items); err != nil {
		return nil, 0, nil, err
	}
	if err = json.Unmarshal(facet, &categories); err != nil {
		return nil, 0, nil, err
	}
	return items, total, categories, nil
}
func (s *Service) searchRecipes(ctx context.Context, tx pgx.Tx, sid string, in SearchInput, take int) ([]map[string]any, int, error) {
	items := []map[string]any{}
	var raw []byte
	var total int
	err := tx.QueryRow(ctx, `WITH matches AS MATERIALIZED (SELECT id,name FROM recipe.recipes WHERE deleted_at IS NULL AND status='published' AND (space_id=$1 OR kind='template') AND ($2='' OR strpos(replace(unaccent(lower(name)),'đ','d'),replace(unaccent(lower($2)),'đ','d'))>0)),page AS (
 SELECT * FROM matches WHERE ($5='' OR (name COLLATE "C",'recipe'::text COLLATE "C",id)>($3 COLLATE "C",$4 COLLATE "C",NULLIF($5,'')::uuid)) ORDER BY name COLLATE "C",id LIMIT $6)
 SELECT COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id::text,'name',name,'type','recipe') ORDER BY name COLLATE "C",id) FROM page),'[]'::jsonb),(SELECT count(*) FROM matches)`, sid, in.Query, in.After.Name, in.After.Type, in.After.ID, take).Scan(&raw, &total)
	if err != nil {
		return nil, 0, err
	}
	if err = json.Unmarshal(raw, &items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
