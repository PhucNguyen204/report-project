package platform

import (
	"dicho/internal/kernel"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestStockConflictIncludesCurrentSnapshotAndTraceWithoutChangingOtherErrors(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("X-Trace-ID", "11111111111111111111111111111111")
	Fail(w, fmt.Errorf("wrapped: %w", &StockConflictError{Current: []StockLevel{{FoodID: "food", LotID: "lot", Dimension: "mass", BaseQuantity: kernel.Quantity(1_500_000)}}}))
	var body struct {
		Code    int          `json:"code"`
		Current []StockLevel `json:"current"`
		Trace   string       `json:"traceId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 409 || body.Code != 4002 || len(body.Current) != 1 || body.Current[0].BaseQuantity != kernel.Quantity(1_500_000) || body.Trace != "11111111111111111111111111111111" {
		t.Fatalf("missing safe conflict snapshot HTTP%d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	Fail(w, Err(422, 2003, "Số lượng vượt lượng còn lại"))
	var ordinary map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &ordinary); err != nil {
		t.Fatal(err)
	}
	if w.Code != 422 || ordinary["code"] != float64(2003) || ordinary["current"] != nil {
		t.Fatal("pantry errors changed with the meal conflict contract")
	}
}

func TestStockConflictWithNoRemainingStockKeepsArrayContract(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, &StockConflictError{})
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	current, ok := body["current"].([]any)
	if w.Code != 409 || !ok || len(current) != 0 {
		t.Fatalf("empty snapshot has invalid contract: %s", w.Body)
	}
}
