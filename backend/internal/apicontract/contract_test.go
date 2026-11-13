package apicontract_test

import (
	"dicho/api"
	"dicho/internal/apicontract"
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
