//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func TestAdminValidationRejectsInvalidCatalogAndConfigWithoutPartialWrites(t *testing.T) {
	f := setupAuth(t)
	token := makeAdmin(t, f, "validation-admin@example.com")
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"suggest.theta", 0.29}, {"suggest.theta", 1.01}, {"suggest.theta", "wrong"}, {"suggest.theta", 0.8},
		{"reminder.runAt", "04:59"}, {"reminder.runAt", "21:01"}, {"reminder.runAt", 21}, {"reminder.runAt", "08:00"},
		{"suggest.weights", map[string]any{"r": 0.6, "e": 0.3}}, {"suggest.weights", map[string]any{"r": -0.1, "e": 1, "f": 0.1}},
		{"suggest.weights", map[string]any{"r": 0.6, "e": 0.3, "f": 0.1}}, {"does.not.exist", 3},
	} {
		status, body := f.call(t, "PUT", "/it4788/v1/admin/configs", map[string]any{"values": map[string]any{tc.key: tc.value}}, token)
		want := 400
		if tc.key == "suggest.theta" && tc.value == 0.8 || tc.key == "reminder.runAt" && tc.value == "08:00" {
			want = 200
		}
		if tc.key == "suggest.weights" {
			if values, ok := tc.value.(map[string]any); ok && values["r"] == 0.6 && len(values) == 3 {
				want = 200
			}
		}
		if status != want {
			t.Fatalf("%s=%v got%d %+v", tc.key, tc.value, status, body)
		}
	}
	for _, in := range []map[string]any{{"name": ""}, {"name": "bad", "dimension": "unknown", "factorToBase": 1}, {"name": "zero", "dimension": "mass", "factorToBase": 0}, {"name": "precise", "dimension": "mass", "factorToBase": 0.00001}, {"name": "bad base", "dimension": "mass", "factorToBase": 2, "isBase": true}} {
		status, _ := f.call(t, "POST", "/it4788/v1/admin/units", in, token)
		if status != 400 {
			t.Fatalf("unit %v status%d", in, status)
		}
	}
	for _, in := range []map[string]any{{"name": "missing unit"}, {"name": "bad status", "categoryId": "00000000-0000-7000-8000-000000000001", "defaultUnitId": "00000000-0000-7000-8000-000000000011", "status": "pending"}, {"name": "bad shelf", "categoryId": "00000000-0000-7000-8000-000000000001", "defaultUnitId": "00000000-0000-7000-8000-000000000011", "shelfLives": map[string]int{"chill": 0}}} {
		status, _ := f.call(t, "POST", "/it4788/v1/admin/foods", in, token)
		if status != 400 {
			t.Fatalf("food %v status%d", in, status)
		}
	}
	var n int
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM catalog.foods WHERE name='bad shelf'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid shelf persisted %d %v", n, err)
	}
	status, _ := f.call(t, "PUT", "/it4788/v1/admin/configs", map[string]any{"values": map[string]any{}}, token)
	if status != 400 {
		t.Fatal(status)
	}
}

func TestFoodMergeSchedulesOneCanonicalMappingAndRejectsInvalidReviews(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "merge-owner@example.com")
	token := makeAdmin(t, f, "merge-admin@example.com")
	var food platform.FoodUnit
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		food, err = f.s.CreatePrivateFood(ctx, tx, u["spaceId"].(string), "Merge herb", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/admin/foods/" + food.FoodID + "/review"
	for _, in := range []map[string]any{{"action": "invalid"}, {"action": "merge", "targetFoodId": food.FoodID}, {"action": "merge", "targetFoodId": platform.ID()}} {
		status, _ := f.call(t, "POST", path, in, token)
		if status != 400 && status != 404 {
			t.Fatalf("invalid review %d", status)
		}
	}
	for i := 0; i < 2; i++ {
		status, body := f.call(t, "POST", path, map[string]any{"action": "merge", "targetFoodId": "00000000-0000-7000-8000-000000000021"}, token)
		want := 200
		if i == 1 {
			want = 409
		}
		if status != want {
			t.Fatalf("merge %d %+v", status, body)
		}
	}
	var target string
	var n int
	if err := f.db.Pool.QueryRow(ctx, `SELECT merged_into::text FROM catalog.foods WHERE id=$1`, food.FoodID).Scan(&target); err != nil || target != "00000000-0000-7000-8000-000000000021" {
		t.Fatalf("mapping %s %v", target, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'event'='food.merged'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("mapping jobs %d %v", n, err)
	}
}

func TestProfileMediaValidationAndGroupConstraintsDoNotPartiallyUpdate(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "media-profile@example.com")
	token := u["accessToken"].(string)
	uid := u["userId"].(string)
	avatar := platform.ID()
	status, _ := f.call(t, "PATCH", "/it4788/v1/me", map[string]any{"locale": "fr"}, token)
	if status != 400 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "PATCH", "/it4788/v1/me", map[string]any{"fullName": "Must rollback", "avatarObjectId": avatar}, token)
	if status != 422 {
		t.Fatal(status)
	}
	f.s.ValidateMedia = func(ctx context.Context, tx pgx.Tx, user, purpose, owner, obj string) error {
		if user != uid || purpose != "avatar" || owner != uid || obj != avatar {
			return platform.Forbidden()
		}
		return nil
	}
	status, body := f.call(t, "PATCH", "/it4788/v1/me", map[string]any{"avatarObjectId": avatar}, token)
	if status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "GET", "/it4788/v1/me", nil, token)
	if body["data"].(map[string]any)["avatarObjectId"] != avatar || body["data"].(map[string]any)["fullName"] != "Nguyễn An" {
		t.Fatal(body)
	}
	status, _ = f.call(t, "PATCH", "/it4788/v1/me", map[string]any{"avatarObjectId": ""}, token)
	if status != 200 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "x"}, token)
	if status != 400 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/spaces/"+u["spaceId"].(string)+"/leave", map[string]any{}, token)
	if status != 422 {
		t.Fatal(status)
	}
	for i := 0; i < 11; i++ {
		status, _ = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": "INVALID"}, token)
		want := 422
		if i == 10 {
			want = 429
		}
		if status != want {
			t.Fatalf("attempt%d status%d", i, status)
		}
	}
	f.clock.at = f.clock.at.Add(time.Hour)
	status, body = f.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": "media-profile@example.com", "password": "ValidPass1"}, "")
	if status != 200 {
		t.Fatal(body)
	}
	token = body["data"].(map[string]any)["accessToken"].(string)
	status, _ = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": "INVALID"}, token)
	if status != 422 {
		t.Fatal(status)
	}
}
