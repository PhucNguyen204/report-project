package exports

import (
	"bytes"
	"dicho/internal/kernel"
	"dicho/internal/modules/planning"
	"encoding/csv"
	"strings"
	"testing"
)

// Without spreadsheet formula neutralization a user-supplied food name executes on CSV open.
func TestCSVPreservesVietnameseAmountsAndNeutralizesFormulas(t *testing.T) {
	report := planning.ReportResult{Type: "spending", From: "2026-11-01", To: "2026-11-12", TotalCost: 12000, Quantities: map[string]kernel.Quantity{"mass": 1_500_000}, TopFoods: []planning.ReportFood{{Name: "Cà rốt, tươi", Dimension: "mass", Quantity: 1_500_000, TotalCost: 12000}, {Name: " =HYPERLINK(\"https://evil.local\")", Dimension: "count", Quantity: 1_000_000}}}
	b, err := encodeCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	foundName, foundFormula, foundQuantity := false, false, false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "Cà rốt, tươi" {
				foundName = true
			}
			if strings.Contains(cell, "HYPERLINK") {
				foundFormula = strings.HasPrefix(cell, "'")
			}
			if cell == "1.5" {
				foundQuantity = true
			}
		}
	}
	if !foundName || !foundFormula || !foundQuantity {
		t.Fatalf("CSV %s", b)
	}
}

// Comparison rows must survive export, including signed numeric deltas that remain usable in a spreadsheet.
func TestCSVIncludesPreviousPeriodAndSignedChanges(t *testing.T) {
	r := planning.ReportResult{Type: "spending", Previous: planning.ReportSummary{From: "2026-10-01", To: "2026-10-31", TotalCost: 20000}, Change: planning.ReportChange{TotalCost: -8000, Quantities: map[string]kernel.Quantity{"mass": -500_000}}}
	b, err := encodeCSV(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Kỳ trước,Tổng chi phí (VND),,20000,,", "Chênh lệch,Tổng chi phí (VND),,-8000,,", "Chênh lệch,Tổng lượng,g,-0.5,,"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %q: %s", want, b)
		}
	}
}

// Dropping breakdown rows or mixing quantities of different dimensions would make downloaded waste data misleading.
func TestCSVWasteIncludesBreakdownsAndSeparateDimensions(t *testing.T) {
	r := planning.ReportResult{Type: "waste", Quantities: map[string]kernel.Quantity{"mass": 1_000_000, "volume": 2_000_000, "count": 3_000_000}, Lots: 2, UnpricedLots: 1, WasteRate: 0.5, Categories: []planning.ReportCategory{{Name: "Rau củ", Quantities: map[string]kernel.Quantity{"mass": 1_000_000}, TotalCost: 5000}}, Reasons: []planning.ReportReason{{Reason: "expired", Lots: 2, TotalCost: 5000}}, TopFoods: []planning.ReportFood{{Name: "Sữa tươi", Dimension: "volume", Quantity: 2_000_000, TotalCost: 5000}}}
	b, err := encodeCSV(r)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"Báo cáo lãng phí", "50.0%", "3 cái; 1 g; 2 ml", "Theo danh mục,Rau củ,g,1,5000,", "Nguyên nhân loại bỏ,expired,,,5000,2", "Thực phẩm nổi bật,Sữa tươi,ml,2,5000,"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	r.Type = "consumption"
	b, err = encodeCSV(r)
	if err != nil || !strings.Contains(string(b), "Báo cáo tiêu thụ") {
		t.Fatalf("consumption %s %v", b, err)
	}
}
