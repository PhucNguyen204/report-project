//go:build integration

package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"dicho/internal/kernel"
	"dicho/internal/modules/accounts"
	"dicho/internal/modules/exports"
	"dicho/internal/modules/inventory"
	"dicho/internal/modules/mealai"
	"dicho/internal/modules/media"
	"dicho/internal/modules/notification"
	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/jackc/pgx/v5"
)

type lifecycleClock struct{ now time.Time }

func (c lifecycleClock) Now() time.Time { return c.now }

type lifecycleFixture struct {
	d                                  *Dispatcher
	user, source, target, food, recipe string
}

func lifecycleSetup(t *testing.T) lifecycleFixture {
	t.Helper()
	ctx := context.Background()
	db := testutil.NewDB(t)
	db.Clock = lifecycleClock{time.Date(2026, 11, 12, 6, 0, 0, 0, time.UTC)}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := accounts.New(db, key)
	inv := inventory.New(db, a)
	p := planning.New(db, inv, inv, a)
	f := lifecycleFixture{d: &Dispatcher{DB: db, Accounts: a, Inventory: inv, Planning: p, Media: media.New(db, nil), Exports: exports.New(db, p, nil), Notifications: notification.New(db, nil, nil), AI: mealai.New(db, p, nil)}, user: platform.ID(), source: platform.ID(), target: platform.ID()}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Dispatcher User','unused','active')`, f.user, f.user+"@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO identity.user_settings(user_id) VALUES($1)`, f.user); err != nil {
		t.Fatal(err)
	}
	for _, space := range []struct{ id, kind string }{{f.source, "personal"}, {f.target, "family"}} {
		if _, err = db.Pool.Exec(ctx, `INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,$2,'Dispatcher space',$3)`, space.id, space.kind, f.user); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Pool.Exec(ctx, `INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader',$3)`, space.id, f.user, space.kind); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		food, err := a.CreatePrivateFood(ctx, tx, f.source, "Private dispatcher food", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		f.food = food.FoodID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	q, _ := kernel.ParseQuantity("2")
	cost := int64(5000)
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, f.source, f.user, []platform.StockInput{{FoodID: f.food, UnitID: "00000000-0000-7000-8000-000000000011", Quantity: q, Location: "chill", ExpiresOn: db.Today().AddDate(0, 0, 3), TotalCost: &cost}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	list, err := inv.CreateList(ctx, f.source, f.user, inventory.ListInput{Name: "Private list", PlannedDate: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := inv.AddItem(ctx, f.source, f.user, list.ID, inventory.ItemInput{FoodID: f.food, UnitID: "00000000-0000-7000-8000-000000000011", Quantity: q})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = inv.Purchase(ctx, f.source, f.user, []inventory.PurchaseInput{{ItemID: item.ID}}); err != nil {
		t.Fatal(err)
	}
	recipe, err := p.CreateRecipe(ctx, f.user, f.source, planning.RecipeInput{Name: "Private recipe", Servings: 2, Ingredients: []planning.IngredientInput{{FoodID: f.food, UnitID: "00000000-0000-7000-8000-000000000011", Quantity: q}}})
	if err != nil {
		t.Fatal(err)
	}
	f.recipe = recipe.ID
	return f
}
func lifecycleEvent(kind, key string, payload any) platform.Event {
	b, _ := json.Marshal(payload)
	return platform.Event{Event: kind, Key: key, Payload: b}
}
func (f lifecycleFixture) copyEvent() platform.Event {
	return lifecycleEvent("space.copy", "space.copy:"+f.target, map[string]any{"sourceSpaceId": f.source, "targetSpaceId": f.target, "userId": f.user, "lists": true, "lots": true, "recipes": true})
}
func (f lifecycleFixture) assertCopied(t *testing.T) {
	t.Helper()
	var lists, lots, recipes, foods, receipts int
	var cost int64
	var state string
	err := f.d.DB.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM shopping.shopping_lists WHERE space_id=$1),(SELECT count(*) FROM pantry.food_lots WHERE space_id=$1),(SELECT count(*) FROM recipe.recipes WHERE space_id=$1),(SELECT count(*) FROM catalog.foods WHERE owner_space_id=$1),(SELECT count(*) FROM platform.worker_receipts WHERE event_key=$2)`, f.target, "space.copy:"+f.target).Scan(&lists, &lots, &recipes, &foods, &receipts)
	if err != nil || lists != 1 || lots != 1 || recipes != 1 || foods != 1 || receipts != 1 {
		t.Fatalf("copy counts %d %d %d %d receipt%d %v", lists, lots, recipes, foods, receipts, err)
	}
	if err = f.d.DB.Pool.QueryRow(context.Background(), `SELECT total_cost FROM pantry.food_lots WHERE space_id=$1`, f.target).Scan(&cost); err != nil || cost != 5000 {
		t.Fatalf("copied cost %d %v", cost, err)
	}
	if err = f.d.DB.Pool.QueryRow(context.Background(), `SELECT status FROM shopping.shopping_items WHERE space_id=$1`, f.target).Scan(&state); err != nil || state != "to_buy" {
		t.Fatalf("copied item state %s %v", state, err)
	}
}

func TestDispatcherCopiesModulesOnceUnderConcurrentDelivery(t *testing.T) {
	f := lifecycleSetup(t)
	e := f.copyEvent()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errs <- f.d.Handle(context.Background(), e) }()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := f.d.Handle(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	f.assertCopied(t)
}
func TestDispatcherRollsBackCopyAndReceiptWhenPlanningFails(t *testing.T) {
	f := lifecycleSetup(t)
	ctx := context.Background()
	if _, err := f.d.DB.Pool.Exec(ctx, `UPDATE catalog.foods SET status='inactive' WHERE id=$1`, f.food); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Handle(ctx, f.copyEvent()); err == nil {
		t.Fatal("inactive recipe ingredient copy succeeded")
	}
	var children, receipts int
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM shopping.shopping_lists WHERE space_id=$1)+(SELECT count(*) FROM pantry.food_lots WHERE space_id=$1)+(SELECT count(*) FROM catalog.foods WHERE owner_space_id=$1),(SELECT count(*) FROM platform.worker_receipts)`, f.target).Scan(&children, &receipts); err != nil || children != 0 || receipts != 0 {
		t.Fatalf("partial copy children%d receipts%d %v", children, receipts, err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `UPDATE catalog.foods SET status='pending_review' WHERE id=$1`, f.food); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Handle(ctx, f.copyEvent()); err != nil {
		t.Fatal(err)
	}
	f.assertCopied(t)
}
func TestDispatcherCopyRejectsForeignSourceRemovedMemberAndMalformedPayload(t *testing.T) {
	f := lifecycleSetup(t)
	ctx := context.Background()
	for _, e := range []platform.Event{
		lifecycleEvent("space.copy", "bad-source", map[string]any{"sourceSpaceId": f.target, "targetSpaceId": f.source, "userId": f.user, "lots": true}),
		lifecycleEvent("space.copy", "foreign-user", map[string]any{"sourceSpaceId": f.source, "targetSpaceId": f.target, "userId": platform.ID(), "lots": true}),
		lifecycleEvent("space.copy", "invalid-id", map[string]any{"sourceSpaceId": "bad", "targetSpaceId": f.target, "userId": f.user, "lots": true}),
		{Event: "space.copy", Key: "malformed", Payload: json.RawMessage(`{"sourceSpaceId":`)},
	} {
		if err := f.d.Handle(ctx, e); err == nil {
			t.Fatalf("invalid copy accepted %+v", e)
		}
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `DELETE FROM household.space_members WHERE space_id=$1`, f.target); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Handle(ctx, f.copyEvent()); err == nil {
		t.Fatal("removed member copied into family")
	}
	var n int
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM platform.worker_receipts`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid jobs acknowledged %d %v", n, err)
	}
}

func TestDispatcherPurgeGuardsActiveSpaceThenRemovesAllChildrenAndQueuesBlobCleanup(t *testing.T) {
	f := lifecycleSetup(t)
	ctx := context.Background()
	if err := f.d.Handle(ctx, f.copyEvent()); err != nil {
		t.Fatal(err)
	}
	e := lifecycleEvent("space.purge", "space.purge:"+f.target, map[string]string{"spaceId": f.target})
	if err := f.d.Handle(ctx, e); err == nil {
		t.Fatal("active family purged")
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO media.objects(id,uploaded_by,purpose,target_id,content_type,size,staging_key,object_key,status,expires_at,completed_at) VALUES($1,$2,'space',$3,'image/png',10,$4,$5,'ready',$6,$7)`, platform.ID(), f.user, f.target, "staging/"+f.target, "media/"+f.target, f.d.DB.Clock.Now().Add(time.Minute), f.d.DB.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO report.exports(id,space_id,requested_by,report_type,from_date,to_date,format,group_by,status,object_key,expires_at,completed_at) VALUES($1,$2,$3,'waste','2026-11-01','2026-11-12','csv','day','ready',$4,$5,$6)`, platform.ID(), f.target, f.user, "exports/"+f.target, f.d.DB.Clock.Now().Add(time.Hour), f.d.DB.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	var copiedFood, copiedRecipe string
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT id::text FROM catalog.foods WHERE owner_space_id=$1`, f.target).Scan(&copiedFood); err != nil {
		t.Fatal(err)
	}
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT id::text FROM recipe.recipes WHERE space_id=$1`, f.target).Scan(&copiedRecipe); err != nil {
		t.Fatal(err)
	}
	for _, object := range []struct{ purpose, target string }{{"food", copiedFood}, {"recipe", copiedRecipe}} {
		id := platform.ID()
		if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO media.objects(id,uploaded_by,purpose,target_id,content_type,size,staging_key,object_key,status,expires_at,completed_at) VALUES($1,$2,$3,$4,'image/png',10,$5,$6,'ready',$7,$8)`, id, f.user, object.purpose, object.target, "staging/"+id, "media/"+id, f.d.DB.Clock.Now().Add(time.Minute), f.d.DB.Clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO recipe.favorite_recipes(user_id,recipe_id) VALUES($1,$2)`, f.user, copiedRecipe); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO household.invite_codes(id,space_id,code,created_by,expires_at) VALUES($1,$2,'PURGETEST',$3,$4)`, platform.ID(), f.target, f.user, f.d.DB.Clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO mealplan.meal_entries(id,space_id,name,meal_date,meal_slot,servings,created_by) VALUES($1,$2,'Meal history','2026-11-12','lunch',2,$3)`, platform.ID(), f.target, f.user); err != nil {
		t.Fatal(err)
	}
	if err := f.d.DB.Tx(ctx, func(tx pgx.Tx) error {
		return f.d.DB.Activity(ctx, tx, f.target, f.user, "test.activity", f.target, map[string]any{})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `UPDATE household.spaces SET dissolved_at=now() WHERE id=$1`, f.target); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.d.Handle(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	var children, jobs, source int
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM household.spaces WHERE id=$1)+(SELECT count(*) FROM catalog.foods WHERE owner_space_id=$1)+(SELECT count(*) FROM shopping.shopping_lists WHERE space_id=$1)+(SELECT count(*) FROM pantry.food_lots WHERE space_id=$1)+(SELECT count(*) FROM recipe.recipes WHERE space_id=$1)+(SELECT count(*) FROM mealplan.meal_entries WHERE space_id=$1)+(SELECT count(*) FROM platform.activities WHERE space_id=$1)+(SELECT count(*) FROM report.exports WHERE space_id=$1)+(SELECT count(*) FROM media.objects),(SELECT count(*) FROM river_job WHERE args->>'event' IN ('media.delete','export.delete')),(SELECT count(*) FROM pantry.food_lots WHERE space_id=$2)`, f.target, f.source).Scan(&children, &jobs, &source); err != nil || children != 0 || jobs < 2 || source != 1 {
		t.Fatalf("purge children%d cleanupjobs%d source%d %v", children, jobs, source, err)
	}
}

func TestDispatcherFoodRemapIsAtomicAcrossInventoryAndPlanning(t *testing.T) {
	f := lifecycleSetup(t)
	ctx := context.Background()
	canonical := "00000000-0000-7000-8000-000000000021"
	e := lifecycleEvent("food.merged", "food.merged:"+f.food, map[string]string{"sourceFoodId": f.food, "targetFoodId": canonical})
	if err := f.d.Handle(ctx, e); err == nil {
		t.Fatal("unapproved food remap accepted")
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `UPDATE catalog.foods SET status='inactive',merged_into=$2 WHERE id=$1`, f.food, canonical); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `UPDATE recipe.recipe_ingredients SET dimension='volume' WHERE recipe_id=$1`, f.recipe); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Handle(ctx, e); err == nil {
		t.Fatal("incompatible planning remap succeeded")
	}
	var old int
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM pantry.food_lots WHERE food_id=$1`, f.food).Scan(&old); err != nil || old != 1 {
		t.Fatalf("inventory remap escaped rollback %d %v", old, err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `UPDATE recipe.recipe_ingredients SET dimension='mass' WHERE recipe_id=$1`, f.recipe); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.d.Handle(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pantry.food_lots WHERE food_id=$1)+(SELECT count(*) FROM pantry.stock_movements WHERE food_id=$1)+(SELECT count(*) FROM shopping.shopping_items WHERE food_id=$1)+(SELECT count(*) FROM recipe.recipe_ingredients WHERE food_id=$1)`, f.food).Scan(&old); err != nil || old != 0 {
		t.Fatalf("stale food refs %d %v", old, err)
	}
}

func TestDispatcherRejectsMissingKeysUnknownEventsAndInvalidPayloadsBeforeDatabase(t *testing.T) {
	d := &Dispatcher{}
	for _, e := range []platform.Event{{Event: "unknown"}, {Event: "space.copy", Payload: json.RawMessage(`{}`)}, {Event: "space.purge", Key: "bad", Payload: json.RawMessage(`{"spaceId":"invalid"}`)}, {Event: "food.merged", Key: "bad", Payload: json.RawMessage(`{}`)}} {
		err := d.Handle(context.Background(), e)
		if err == nil {
			t.Fatalf("invalid event accepted %+v", e)
		}
		var app *platform.Error
		if e.Event != "unknown" && !errors.As(err, &app) {
			t.Fatalf("payload error not deterministic: %v", err)
		}
	}
	if err := d.Handle(context.Background(), platform.Event{Event: "settings.changed", Payload: json.RawMessage(`{"userId":"` + platform.ID() + `"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := d.Handle(context.Background(), platform.Event{Event: "unknown"}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatal(err)
	}
}

func TestDispatcherAnonymizationAtomicallyPurgesMediaAndPersonalInbox(t *testing.T) {
	f := lifecycleSetup(t)
	ctx := context.Background()
	object := platform.ID()
	notice := platform.ID()
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO media.objects(id,uploaded_by,purpose,target_id,content_type,size,staging_key,object_key,status,expires_at,completed_at) VALUES($1,$2,'avatar',$2,'image/png',10,$3,$4,'ready',$5,$6)`, object, f.user, "staging/"+object, "media/"+object, f.d.DB.Clock.Now().Add(time.Minute), f.d.DB.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type) VALUES($1,$2,$3,'Private inbox','Private body','activity')`, notice, f.user, notice); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO notification.deliveries(notification_id,device_id) VALUES($1,$2)`, notice, platform.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `CREATE FUNCTION notification.test_reject_purge() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'cleanup unavailable'; END; $$; CREATE TRIGGER test_reject_purge BEFORE DELETE ON notification.notifications FOR EACH ROW EXECUTE FUNCTION notification.test_reject_purge()`); err != nil {
		t.Fatal(err)
	}
	e := lifecycleEvent("account.anonymized", "anon:"+f.user, map[string]string{"userId": f.user})
	if err := f.d.Handle(ctx, e); err == nil {
		t.Fatal("inbox cleanup failure hidden")
	}
	var objects, notices, jobs int
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM media.objects WHERE id=$1),(SELECT count(*) FROM notification.notifications WHERE id=$2),(SELECT count(*) FROM river_job WHERE args->>'event'='media.delete')`, object, notice).Scan(&objects, &notices, &jobs); err != nil || objects != 1 || notices != 1 || jobs != 0 {
		t.Fatalf("anonymization partially committed media%d notices%d jobs%d %v", objects, notices, jobs, err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `DROP TRIGGER test_reject_purge ON notification.notifications; DROP FUNCTION notification.test_reject_purge()`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.d.Handle(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	var deliveries int
	if err := f.d.DB.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM media.objects WHERE id=$1),(SELECT count(*) FROM notification.notifications WHERE id=$2),(SELECT count(*) FROM notification.deliveries WHERE notification_id=$2),(SELECT count(*) FROM river_job WHERE args->>'event'='media.delete')`, object, notice).Scan(&objects, &notices, &deliveries, &jobs); err != nil || objects != 0 || notices != 0 || deliveries != 0 || jobs != 2 {
		t.Fatalf("privacy cleanup media%d notices%d deliveries%d jobs%d %v", objects, notices, deliveries, jobs, err)
	}
}

func TestDispatcherAcknowledgesLiveConfigurationAndRejectsReceiptKindCollision(t *testing.T) {
	d := &Dispatcher{}
	if err := d.Handle(context.Background(), lifecycleEvent("config.changed", "config:1", map[string]any{"values": map[string]int{"reminder.defaultDays": 4}})); err != nil {
		t.Fatal(err)
	}
	f := lifecycleSetup(t)
	ctx := context.Background()
	if err := f.d.Handle(ctx, f.copyEvent()); err != nil {
		t.Fatal(err)
	}
	e := lifecycleEvent("space.purge", f.copyEvent().Key, map[string]string{"spaceId": f.target})
	var typed *platform.Error
	if err := f.d.Handle(ctx, e); !errors.As(err, &typed) || typed.Status != 409 {
		t.Fatalf("kind collision accepted %v", err)
	}
}

func TestDispatcherCanonicalMappingAcceptsUppercaseUUIDPayload(t *testing.T) {
	f := lifecycleSetup(t)
	ctx := context.Background()
	canonical := platform.ID()
	if _, err := f.d.DB.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,'Uppercase target','00000000-0000-7000-8000-000000000001','00000000-0000-7000-8000-000000000011')`, canonical); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.DB.Pool.Exec(ctx, `UPDATE catalog.foods SET status='inactive',merged_into=$2 WHERE id=$1`, f.food, canonical); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Handle(ctx, lifecycleEvent("food.merged", "uppercase:"+f.food, map[string]string{"sourceFoodId": strings.ToUpper(f.food), "targetFoodId": strings.ToUpper(canonical)})); err != nil {
		t.Fatalf("equivalent canonical UUID rejected: %v", err)
	}
}

func TestDispatcherValidatesAccountPurgeBeforeCallingModuleSQL(t *testing.T) {
	f := lifecycleSetup(t)
	err := f.d.Handle(context.Background(), lifecycleEvent("account.purge", "account:bad", map[string]string{"userId": "not-a-uuid"}))
	var typed *platform.Error
	if !errors.As(err, &typed) || typed.Status != 400 {
		t.Fatalf("account payload reached SQL rather than deterministic validation: %v", err)
	}
}
