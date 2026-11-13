package api_test

import (
	"dicho/api"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestDocumentValidatesAgainstOfficialOpenAPI31SchemaAndResolvesComponents(t *testing.T) {
	var doc any
	if err := json.Unmarshal(api.Document, &doc); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/openapi-3.1-schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var meta any
	if err = json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	if err = c.AddResource("https://spec.openapis.org/oas/3.1/schema/2025-09-15", meta); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("https://spec.openapis.org/oas/3.1/schema/2025-09-15")
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(doc); err != nil {
		t.Fatal(err)
	}
	if err = c.AddResource("https://dicho.example/openapi.json", doc); err != nil {
		t.Fatal(err)
	}
	root := doc.(map[string]any)
	for name := range root["components"].(map[string]any)["schemas"].(map[string]any) {
		if _, err = c.Compile("https://dicho.example/openapi.json#/components/schemas/" + name); err != nil {
			t.Fatalf("schema %s: %v", name, err)
		}
	}
}

func TestDocumentCoversEveryInScopeAPICodeWithExplicitSchemasAndHeaders(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(api.Document, &root); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	count := 0
	for path, item := range root["paths"].(map[string]any) {
		for method, value := range item.(map[string]any) {
			if !strings.Contains(" get post put patch delete head options ", " "+method+" ") {
				continue
			}
			op := value.(map[string]any)
			count++
			id := op["operationId"].(string)
			if ids[id] {
				t.Fatalf("duplicate operationId %s", id)
			}
			ids[id] = true
			code, ok := op["x-api-code"].(string)
			if !ok {
				t.Fatalf("unmapped %s %s", method, path)
			}
			seen[code] = true
			if _, ok := op["x-use-case"].([]any); !ok {
				t.Fatalf("missing use-case mapping %s", id)
			}
			for status, response := range op["responses"].(map[string]any) {
				if status == "204" || status == "304" {
					continue
				}
				if _, ok := response.(map[string]any)["$ref"]; !ok {
					if _, ok := response.(map[string]any)["content"]; !ok {
						t.Fatalf("untyped response %s", id)
					}
				}
			}
		}
	}
	for prefix, max := range map[string]int{"A": 18, "G": 10, "S": 15, "P": 7, "R": 9, "M": 6, "Q": 3, "B": 5, "D": 11} {
		for n := 1; n <= max; n++ {
			code := fmt.Sprintf("%s%02d", prefix, n)
			if !seen[code] {
				t.Errorf("missing API %s", code)
			}
		}
	}
	for _, code := range []string{"Y03", "Y04"} {
		if !seen[code] {
			t.Errorf("missing API %s", code)
		}
	}
	for _, code := range []string{"Y01", "Y02", "P08"} {
		if seen[code] {
			t.Fatalf("out-of-scope operation %s", code)
		}
	}
	if count < 100 {
		t.Fatalf("incomplete application operation set: %d", count)
	}
}
