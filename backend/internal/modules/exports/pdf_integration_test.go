//go:build integration

package exports

import (
	"dicho/internal/kernel"
	"dicho/internal/modules/planning"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Missing Unicode embedding or pagination would corrupt Vietnamese labels or drop the report's final rows.
func TestPDFEmbedsVietnameseWrapsAndPaginates(t *testing.T) {
	report := planning.ReportResult{Type: "spending", From: "2026-01-01", To: "2026-12-31", TotalCost: 12000, Quantities: map[string]kernel.Quantity{"mass": 1_500_000}, TopFoods: []planning.ReportFood{{Name: "Cà rốt Đà Lạt tươi ngon dùng cho bữa cơm gia đình và món canh rau củ bổ dưỡng mỗi ngày", Dimension: "mass", Quantity: 1_500_000, TotalCost: 12000}}}
	report.Previous = planning.ReportSummary{From: "2025-01-01", To: "2025-12-31", TotalCost: 14000, Quantities: map[string]kernel.Quantity{"mass": 2_000_000}}
	report.Change = planning.ReportChange{TotalCost: -2000, Quantities: map[string]kernel.Quantity{"mass": -500_000}}
	for day := 0; day < 365; day++ {
		report.Series = append(report.Series, planning.ReportPoint{Period: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, day).Format("2006-01-02"), TotalCost: int64(day), Quantities: map[string]kernel.Quantity{"mass": 1_500_000}})
	}
	b, err := encodePDF(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 1000 {
		t.Fatalf("missing PDF: %d bytes", len(b))
	}
	dir := t.TempDir()
	if v := os.Getenv("EXPORT_QA_DIR"); v != "" {
		dir = v
		if err = os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "vietnamese-export.pdf")
	if err = os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", path, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("PDF text extraction: %v %s", err, text)
	}
	compact := strings.Join(strings.Fields(string(text)), " ")
	for _, want := range []string{"Đi chợ tiện lợi", "Cà rốt Đà Lạt", "bổ dưỡng mỗi ngày", "2026-12-31", "12000", "Chênh lệch", "-2000", "2025-01-01"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("missing %q: %s", want, compact)
		}
	}
	info, err := exec.Command("pdfinfo", path).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	pages := 0
	for _, line := range strings.Split(string(info), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			_, _ = fmt.Sscanf(line, "Pages: %d", &pages)
		}
	}
	if pages < 2 {
		t.Fatalf("report not paginated: %s", info)
	}
	t.Logf("PDF %s; pages=%d; bytes=%d", path, pages, len(b))
}
