package apicontract_test

import (
	"dicho/api"
	"dicho/internal/apicontract"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestContractRejectsMissingRequiredFieldsAndWrongTypes(t *testing.T) {
	c, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/00000000-0000-7000-8000-000000000001/recipes"
	req := httptest.NewRequest("POST", path, nil)
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("X-App-Version", "1.0.0")
	req.Header.Set("Idempotency-Key", "00000000-0000-7000-8000-000000000001")
	req.Header.Set("Content-Type", "application/json")
	for name, body := range map[string]string{"missing ingredients": `{"name":"Canh","servings":2}`, "wrong servings type": `{"name":"Canh","servings":"2","ingredients":[]}`, "excess quantity precision": `{"name":"Canh","servings":2,"ingredients":[{"foodId":"00000000-0000-7000-8000-000000000021","unitId":"00000000-0000-7000-8000-000000000012","quantity":"0.001"}]}`} {
		if err = c.Request(req, []byte(body)); err == nil {
			t.Errorf("invalid %s accepted", name)
		}
	}
	valid := []byte(`{"name":"Canh","servings":2,"ingredients":[{"foodId":"00000000-0000-7000-8000-000000000021","unitId":"00000000-0000-7000-8000-000000000012","quantity":"0.2"}]}`)
	if err = c.Request(req, valid); err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Idempotency-Key", "create-recipe")
	if err = c.Request(req, valid); err == nil {
		t.Fatal("malformed mutation UUID accepted")
	}
	req.Header.Del("Idempotency-Key")
	if err = c.Request(req, valid); err == nil {
		t.Fatal("missing mutation key accepted")
	}
	for _, body := range []string{`{"code":1000,"meta":{"nextCursor":"","serverTime":"2026-11-13T00:00:00Z"}}`, `{"code":"1000","data":{"items":[]},"meta":{"nextCursor":"","serverTime":"2026-11-13T00:00:00Z"}}`, `{"code":1000,"data":{"items":[{"name":"Canh"}]},"meta":{"nextCursor":"","serverTime":"2026-11-13T00:00:00Z"}}`} {
		if err = c.Response("GET", path, 200, nil, []byte(body)); err == nil {
			t.Fatalf("invalid response accepted %s", body)
		}
	}
	if err = c.Response("GET", path, 200, nil, []byte(`{"code":1000,"data":{"items":[]},"meta":{"nextCursor":"","serverTime":"2026-11-13T00:00:00Z"}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestStaticContractAndHealthDoNotRequireApplicationHeaders(t *testing.T) {
	c, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz", "/openapi.json"} {
		req := httptest.NewRequest("GET", path, nil)
		if err := c.Request(req, nil); err != nil {
			t.Fatalf("public %s: %v", path, err)
		}
	}
}

func TestErrorContractAllowsValidOptionalTraceCorrelation(t *testing.T) {
	c, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"code":3001,"message":"unauthorized"}`, `{"code":3001,"message":"unauthorized","traceId":"11111111111111111111111111111111"}`} {
		if err := c.Response("GET", "/it4788/v1/me", 401, nil, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Response("GET", "/it4788/v1/me", 401, nil, []byte(`{"code":3001,"message":"unauthorized","traceId":"invalid"}`)); err == nil {
		t.Fatal("invalid trace correlation accepted")
	}
}

func TestCollectionContractsRequirePaginationMetadata(t *testing.T) {
	c, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/it4788/v1/spaces/00000000-0000-7000-8000-000000000001/recipes",
		"/it4788/v1/spaces/00000000-0000-7000-8000-000000000001/meals",
		"/it4788/v1/admin/recipe-templates", "/it4788/v1/admin/users", "/it4788/v1/admin/audit-logs",
		"/it4788/v1/admin/foods", "/it4788/v1/admin/food-categories", "/it4788/v1/admin/units",
	} {
		t.Run(path, func(t *testing.T) {
			if err := c.Response("GET", path, 200, nil, []byte(`{"code":1000,"data":{"items":[]}}`)); err == nil {
				t.Fatal("response without pagination metadata accepted")
			}
			if err := c.Response("GET", path, 200, nil, []byte(`{"code":1000,"data":{"items":[]},"meta":{"nextCursor":"","serverTime":"2026-11-13T00:00:00Z"}}`)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSearchContractIncludesTypedFacetsAndCursorMetadata(t *testing.T) {
	c, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/00000000-0000-7000-8000-000000000001/search"
	valid := `{"code":1000,"data":{"items":[],"facets":{"food":0,"recipe":0,"lot":0},"filterFacets":{"categoryId":{},"location":{},"expiryState":{}}},"meta":{"nextCursor":"","serverTime":"2026-11-13T00:00:00Z"}}`
	if err := c.Response("GET", path, 200, nil, []byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"code":1000,"data":{"items":[],"facets":{},"filterFacets":{"categoryId":{},"location":{},"expiryState":{}}}}`,
		`{"code":1000,"data":{"items":[],"facets":{}},"meta":{"nextCursor":"","serverTime":"2026-11-13T00:00:00Z"}}`,
	} {
		if err := c.Response("GET", path, 200, nil, []byte(invalid)); err == nil {
			t.Fatalf("incomplete search metadata accepted %s", invalid)
		}
	}
}

func TestCreateContractsValidateClientUUID7WithoutRestrictingDeviceUUIDs(t *testing.T) {
	c, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-7000-8000-000000000001"
	base := "/it4788/v1/spaces/" + id
	ingredients := []map[string]any{{"foodId": id, "unitId": id, "quantity": "1"}}
	for _, tt := range []struct {
		path string
		body map[string]any
	}{
		{base + "/lots", map[string]any{"foodId": id, "unitId": id, "quantity": "1", "storageLocation": "chill"}},
		{base + "/shopping-lists", map[string]any{"name": "Client list"}},
		{base + "/shopping-lists/" + id + "/items", map[string]any{"foodId": id, "unitId": id, "quantity": "1"}},
		{base + "/shopping-lists/" + id + "/copy", map[string]any{"name": "Client copy"}},
		{base + "/recipes", map[string]any{"name": "Client recipe", "servings": 1, "ingredients": ingredients}},
		{base + "/meals", map[string]any{"name": "Client meal", "date": "2026-11-13", "mealSlot": "lunch", "servings": 1}},
		{"/it4788/v1/admin/recipe-templates", map[string]any{"name": "Client template", "servings": 1, "ingredients": ingredients}},
	} {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("POST", tt.path, nil)
			req.Header.Set("Authorization", "Bearer test")
			req.Header.Set("X-App-Version", "1.0.0")
			req.Header.Set("Idempotency-Key", id)
			req.Header.Set("Content-Type", "application/json")
			for _, value := range []any{id, "", nil} {
				tt.body["id"] = value
				raw, _ := json.Marshal(tt.body)
				if err := c.Request(req, raw); err != nil {
					t.Errorf("valid/omitted client ID rejected %v: %v", value, err)
				}
			}
			for _, bad := range []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-7000-0000-000000000001", "bad-id"} {
				tt.body["id"] = bad
				raw, _ := json.Marshal(tt.body)
				if err := c.Request(req, raw); err == nil {
					t.Errorf("invalid resource ID accepted %s", bad)
				}
			}
		})
	}
	req := httptest.NewRequest("POST", "/it4788/v1/auth/login", nil)
	req.Header.Set("X-App-Version", "1.0.0")
	req.Header.Set("Content-Type", "application/json")
	if err := c.Request(req, []byte(`{"email":"member@example.test","password":"valid","deviceId":"00000000-0000-4000-8000-000000000001"}`)); err != nil {
		t.Fatalf("device UUIDv4 was incorrectly restricted: %v", err)
	}
}
