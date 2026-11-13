package platform

import (
	"bytes"
	"dicho/internal/kernel"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func FuzzDecodeStrictObject(f *testing.F) {
	for _, seed := range []string{
		`{"name":"cà chua","quantity":1.25}`, `{"quantity":"0.333333"}`,
		`{"unknown":true}`, `{"quAntitY":0}`, `{} {}`, `null`, `[]`, `{"quantity":null}`, ``,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 4096 {
			return
		}
		var dst struct {
			Name     string          `json:"name"`
			Quantity kernel.Quantity `json:"quantity"`
		}
		err := Decode(httptest.NewRequest("POST", "/", bytes.NewReader(body)), &dst)
		if err != nil {
			return
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(body, &object); err != nil || object == nil {
			t.Fatalf("accepted non-object or trailing JSON: %q", body)
		}
		for key := range object {
			if !strings.EqualFold(key, "name") && !strings.EqualFold(key, "quantity") {
				t.Fatalf("accepted unknown field %q", key)
			}
		}
		got, err := kernel.ParseQuantity(dst.Quantity.String())
		if err != nil || got != dst.Quantity {
			t.Fatalf("quantity lost precision: %q", body)
		}
	})
}
