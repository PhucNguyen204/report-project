package inventory

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func (s *Service) SearchLotsPage(ctx context.Context, tx pgx.Tx, sid, user string, in platform.LotSearchQuery) (platform.LotSearchPage, error) {
	out := platform.LotSearchPage{Items: []map[string]any{}, CategoryCounts: map[string]int{}, LocationCounts: map[string]int{}, ExpiryCounts: map[string]int{}}
	if in.Limit < 1 || in.Limit > 101 {
		return out, platform.Invalid("Giới hạn tìm kiếm không hợp lệ")
	}
	if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
		return out, err
	}
	n := s.reminderDays(ctx, tx, user)
	var items, categories, locations, states []byte
	err := tx.QueryRow(ctx, `WITH matches AS MATERIALIZED (
 SELECT l.id,l.food_id,f.name,l.remaining_qty,l.unit_id,l.expires_on,l.storage_location,f.category_id::text AS category,
 CASE WHEN l.expires_on<$6::date THEN 'expired' WHEN l.expires_on<=$6::date+$7::int THEN 'expiring' ELSE 'fresh' END AS state
 FROM pantry.food_lots l JOIN catalog.foods f ON f.id=l.food_id
 WHERE l.space_id=$1 AND l.deleted_at IS NULL AND l.lifecycle='active'
 AND ($2='' OR strpos(replace(unaccent(lower(f.name)),'đ','d'),replace(unaccent(lower($2)),'đ','d'))>0)
 AND (COALESCE(cardinality($3::uuid[]),0)=0 OR f.category_id=ANY($3))
 AND (COALESCE(cardinality($4::text[]),0)=0 OR l.storage_location=ANY($4))
 AND (COALESCE(cardinality($5::text[]),0)=0 OR (CASE WHEN l.expires_on<$6::date THEN 'expired' WHEN l.expires_on<=$6::date+$7::int THEN 'expiring' ELSE 'fresh' END)=ANY($5))
 ),page AS (
 SELECT * FROM matches WHERE ($10='' OR (name COLLATE "C",'lot'::text COLLATE "C",id)>($8 COLLATE "C",$9 COLLATE "C",NULLIF($10,'')::uuid)) ORDER BY name COLLATE "C",id LIMIT $11
 ) SELECT COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id::text,'foodId',food_id::text,'name',name,'quantity',remaining_qty::text,'unitId',unit_id::text,'expiresOn',to_char(expires_on,'YYYY-MM-DD'),'location',storage_location,'type','lot') ORDER BY name COLLATE "C",id) FROM page),'[]'::jsonb),
 (SELECT count(*) FROM matches),
 COALESCE((SELECT jsonb_object_agg(category,n) FROM (SELECT category,count(*) AS n FROM matches GROUP BY category)x),'{}'::jsonb),
 COALESCE((SELECT jsonb_object_agg(storage_location,n) FROM (SELECT storage_location,count(*) AS n FROM matches GROUP BY storage_location)x),'{}'::jsonb),
 COALESCE((SELECT jsonb_object_agg(state,n) FROM (SELECT state,count(*) AS n FROM matches GROUP BY state)x),'{}'::jsonb)`, sid, in.Query, in.CategoryIDs, in.Locations, in.ExpiryStates, s.db.Today().Format("2006-01-02"), n, in.After.Name, in.After.Type, in.After.ID, in.Limit).Scan(&items, &out.Total, &categories, &locations, &states)
	if err != nil {
		return out, err
	}
	for _, v := range []struct {
		raw    []byte
		target any
	}{{items, &out.Items}, {categories, &out.CategoryCounts}, {locations, &out.LocationCounts}, {states, &out.ExpiryCounts}} {
		if err = json.Unmarshal(v.raw, v.target); err != nil {
			return out, err
		}
	}
	return out, nil
}
