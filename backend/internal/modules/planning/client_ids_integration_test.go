//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"net/http"
	"testing"
)

func TestPlanningClientIDsAreValidatedAndPreserved(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	admin := planningPageAdmin(t, db)
	mux := http.NewServeMux()
	s.Register(mux)
	for _, kind := range []string{"recipe", "meal", "template"} {
		t.Run(kind, func(t *testing.T) {
			path := "/it4788/v1/spaces/" + sid + "/recipes"
			actor := platform.Principal{UserID: user}
			body := map[string]any{"name": "Client recipe", "servings": 1, "ingredients": []map[string]any{{"foodId": food, "unitId": unit, "quantity": "1"}}}
			if kind == "meal" {
				path = "/it4788/v1/spaces/" + sid + "/meals"
				body = map[string]any{"name": "Client meal", "date": db.Today().Format("2006-01-02"), "mealSlot": "lunch", "servings": 1}
			}
			if kind == "template" {
				path = "/it4788/v1/admin/recipe-templates"
				actor = platform.Principal{UserID: admin, SystemRole: "admin"}
				body["status"] = "published"
			}
			for _, bad := range []string{"00000000-0000-4000-8000-000000000099", "00000000-0000-7000-0000-000000000099", "bad-id"} {
				body["id"] = bad
				var before, after int
				count := `SELECT (SELECT count(*) FROM recipe.recipes)+(SELECT count(*) FROM recipe.recipe_ingredients)+(SELECT count(*) FROM mealplan.meal_entries)+(SELECT count(*) FROM platform.activities)+(SELECT count(*) FROM admin.audit_logs)`
				if err := db.Pool.QueryRow(context.Background(), count).Scan(&before); err != nil {
					t.Fatal(err)
				}
				status, response := planningRequest(t, mux, actor, "POST", path, body, "")
				if status != 400 {
					t.Errorf("bad%s ID returned%d %+v", kind, status, response)
				}
				if err := db.Pool.QueryRow(context.Background(), count).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if before != after {
					t.Errorf("invalid ID changed planning counts%d→%d", before, after)
				}
			}
			id := platform.ID()
			body["id"] = id
			status, response := planningRequest(t, mux, actor, "POST", path, body, "")
			if status != 201 {
				t.Fatalf("create%s returned%d %+v", kind, status, response)
			}
			if response["data"].(map[string]any)["id"] != id {
				t.Errorf("client ID ignored %+v", response)
			}
			status, response = planningRequest(t, mux, actor, "POST", path, body, "")
			if status != 409 || response["code"] != float64(4003) {
				t.Fatalf("duplicate%s ID returned%d %+v", kind, status, response)
			}
		})
	}
}

func TestTemplateClientIDSurvivesTransactionRetry(t *testing.T) {
	s, db, _, _, unit, food := setupPlanning(t)
	ctx := context.Background()
	admin := planningPageAdmin(t, db)
	if _, err := db.Pool.Exec(ctx, `CREATE SEQUENCE recipe.template_retry_attempt;
 CREATE FUNCTION recipe.fail_first_template_insert() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF NEW.kind='template' AND nextval('recipe.template_retry_attempt')=1 THEN
 RAISE EXCEPTION 'retry fixture' USING ERRCODE='55P03';
 END IF;
 RETURN NEW;
 END; $$;
 CREATE TRIGGER first_template_insert BEFORE INSERT ON recipe.recipes FOR EACH ROW EXECUTE FUNCTION recipe.fail_first_template_insert();`); err != nil {
		t.Fatal(err)
	}
	id := platform.ID()
	in := TemplateInput{RecipeInput: RecipeInput{ID: id, Name: "Stable creation identity", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}}}
	created, err := s.SaveTemplate(ctx, admin, "", 0, in)
	if err != nil || created.ID != id {
		t.Fatalf("retry lost create identity %+v error%v", created, err)
	}
	var attempts, recipes, ingredients, audits int
	if err := db.Pool.QueryRow(ctx, "SELECT (SELECT last_value FROM recipe.template_retry_attempt),(SELECT count(*) FROM recipe.recipes),(SELECT count(*) FROM recipe.recipe_ingredients),(SELECT count(*) FROM admin.audit_logs)").Scan(&attempts, &recipes, &ingredients, &audits); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || recipes != 1 || ingredients != 1 || audits != 1 {
		t.Fatalf("retry effects attempts%d recipes%d ingredients%d audits%d", attempts, recipes, ingredients, audits)
	}
}
