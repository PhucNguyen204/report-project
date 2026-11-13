//go:build integration

package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func movementPage(t *testing.T, w *httptest.ResponseRecorder) (Lot, []Movement, string) {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("history status%d %s", w.Code, w.Body)
	}
	var out struct {
		Data struct {
			Lot
			Movements []Movement `json:"movements"`
		} `json:"data"`
		Meta struct {
			NextCursor string    `json:"nextCursor"`
			ServerTime time.Time `json:"serverTime"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Meta.ServerTime.IsZero() {
		t.Fatal("history lacks serverTime")
	}
	if w.Header().Get("ETag") != fmt.Sprintf("\"%d\"", out.Data.Version) {
		t.Fatal("lot ETag changed during pagination")
	}
	return out.Data.Lot, out.Data.Movements, out.Meta.NextCursor
}

func TestLotMovementHistoryPaginatesBeyondOneThousand(t *testing.T) {
	f := setup(t)
	lot := f.stock(t, "2", "2026-11-12")
	ctx := context.Background()
	stamp := f.db.Clock.Now().Add(-time.Hour)
	_, err := f.db.Pool.Exec(ctx, `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,occurred_at)
 SELECT ('10000000-0000-7000-8000-'||lpad(i::text,12,'0'))::uuid,$1,$2,$3,'adjust',CASE WHEN i%2=0 THEN -1 ELSE 1 END,$4 FROM generate_series(1,1002) i`, f.sid, lot.ID, f.food, stamp)
	if err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/" + f.sid + "/lots/" + lot.ID
	w := api(t, f, "GET", path, "", f.uid, "")
	var raw struct {
		Data struct {
			Movements []Movement `json:"movements"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Data.Movements) != 20 {
		t.Fatalf("default history returned%d movements, want20", len(raw.Data.Movements))
	}
	_, first, next := movementPage(t, w)
	if len(first) != 20 || next == "" {
		t.Fatalf("history default count%d cursor%q", len(first), next)
	}
	for i, m := range first {
		if m.ID != fmt.Sprintf("10000000-0000-7000-8000-%012d", i+1) || !m.OccurredAt.Equal(stamp) {
			t.Fatalf("timestamp/ID ordering %+v", first)
		}
	}
	_, err = f.db.Pool.Exec(ctx, `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,occurred_at)
 VALUES($1,$3,$4,$5,'adjust',1,$6),($2,$3,$4,$5,'adjust',-1,$6)`, platform.ID(), platform.ID(), f.sid, lot.ID, f.food, stamp.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var sum kernel.Quantity
	movements := first
	for page := 0; ; page++ {
		for _, m := range movements {
			if seen[m.ID] || m.UnitID != f.unit {
				t.Fatalf("duplicate or wrong base unit %+v", m)
			}
			seen[m.ID] = true
			sum += m.Quantity
		}
		if next == "" {
			break
		}
		if page > 11 {
			t.Fatal("cursor did not terminate")
		}
		current, more, cursor := movementPage(t, api(t, f, "GET", path+"?limit=100&cursor="+url.QueryEscape(next), "", f.uid, ""))
		if current.ID != lot.ID || current.RemainingQty != lot.RemainingQty || len(more) > 100 {
			t.Fatalf("lot or page changed %+v count%d", current, len(more))
		}
		movements, next = more, cursor
	}
	if len(seen) != 1003 || sum != qty("2") {
		t.Fatalf("history truncated or quantities lost: count%d sum%s", len(seen), sum)
	}
}

func TestLotMovementCursorKeepsLotAndActorScope(t *testing.T) {
	f := setup(t)
	lot := f.stock(t, "1", "2026-11-12")
	otherLot := f.stock(t, "1", "2026-11-12")
	if _, err := f.db.Pool.Exec(context.Background(), `INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity) VALUES($1,$2,$3,$4,'adjust',1)`, platform.ID(), f.sid, lot.ID, f.food); err != nil {
		t.Fatal(err)
	}
	base := "/it4788/v1/spaces/" + f.sid + "/lots/"
	_, _, cursor := movementPage(t, api(t, f, "GET", base+lot.ID+"?limit=1", "", f.uid, ""))
	if cursor == "" {
		t.Fatal("no history continuation")
	}
	for _, path := range []string{base + lot.ID + "?limit=101", base + lot.ID + "?limit=1&limit=2", base + lot.ID + "?cursor=bad", base + otherLot.ID + "?cursor=" + url.QueryEscape(cursor), base + lot.ID + "?q=changed&cursor=" + url.QueryEscape(cursor)} {
		if w := api(t, f, "GET", path, "", f.uid, ""); w.Code != 400 {
			t.Fatalf("invalid cursor accepted%d %s", w.Code, w.Body)
		}
	}
	if w := api(t, f, "GET", base+lot.ID, "", f.other, ""); w.Code != 404 {
		t.Fatalf("outsider history%d %s", w.Code, w.Body)
	}
	if _, err := f.db.Pool.Exec(context.Background(), `INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','family')`, f.sid, f.other); err != nil {
		t.Fatal(err)
	}
	if w := api(t, f, "GET", base+lot.ID+"?cursor="+url.QueryEscape(cursor), "", f.other, ""); w.Code != 400 {
		t.Fatalf("another member reused cursor%d %s", w.Code, w.Body)
	}
}
