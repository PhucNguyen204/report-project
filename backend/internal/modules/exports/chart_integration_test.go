//go:build integration

package exports

import (
	"dicho/internal/kernel"
	"dicho/internal/modules/planning"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func chartPDF(t *testing.T, name string, r planning.ReportResult) (string, string) {
	t.Helper()
	data, err := encodePDF(r)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if configured := os.Getenv("EXPORT_QA_DIR"); configured != "" {
		dir = configured
		if err = os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, name+".pdf")
	if err = os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", path, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("text extraction %v %s", err, text)
	}
	return path, strings.Join(strings.Fields(string(text)), " ")
}

func TestPDFChartsContainRealBarsAndSeparateUnitLabels(t *testing.T) {
	for _, kind := range []string{"spending", "consumption", "waste"} {
		t.Run(kind, func(t *testing.T) {
			r := planning.ReportResult{Type: kind, From: "2026-11-01", To: "2026-11-03", TotalCost: 6000, Quantities: map[string]kernel.Quantity{"mass": 6_000_000, "volume": 12_000_000, "count": 18_000_000}}
			for i := 1; i <= 3; i++ {
				r.Series = append(r.Series, planning.ReportPoint{Period: fmt.Sprintf("2026-11-%02d", i), TotalCost: int64(i * 1000), Quantities: map[string]kernel.Quantity{"mass": kernel.Quantity(i * 1_000_000), "volume": kernel.Quantity(i * 2_000_000), "count": kernel.Quantity(i * 3_000_000)}})
			}
			path, text := chartPDF(t, "chart-"+kind, r)
			title := map[string]string{"spending": "Xu hướng chi tiêu", "consumption": "Xu hướng tiêu thụ", "waste": "Xu hướng lãng phí"}[kind]
			for _, want := range []string{title, "2026-11-01", "2026-11-03", "Tổng quan", "Diễn biến theo thời gian"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q in %s", want, text)
				}
			}
			units := []string{"VND"}
			if kind != "spending" {
				units = []string{"g", "ml", "cái"}
			}
			for _, unit := range units {
				if !strings.Contains(text, "Đơn vị: "+unit) {
					t.Fatalf("missing separate unit%s in %s", unit, text)
				}
			}
			prefix := strings.TrimSuffix(path, ".pdf")
			if output, err := exec.Command("pdftoppm", "-f", "1", "-singlefile", "-r", "72", "-png", path, prefix).CombinedOutput(); err != nil {
				t.Fatalf("raster %v %s", err, output)
			}
			file, err := os.Open(prefix + ".png")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			img, err := png.Decode(file)
			if err != nil {
				t.Fatal(err)
			}
			groups, inside := 0, false
			for x := 48; x < 548; x++ {
				filled := 0
				for y := 145; y < 285; y++ {
					red, green, blue, _ := img.At(x, y).RGBA()
					if red < 60*257 && green > 100*257 && green < 190*257 && blue > 70*257 && blue < 160*257 {
						filled++
					}
				}
				bar := filled >= 5
				if bar && !inside {
					groups++
				}
				inside = bar
			}
			if groups != 3 {
				t.Fatalf("rendered plot has%d bars, want3; labels alone are insufficient", groups)
			}
		})
	}
}

func TestPDFChartsKeepLongSeriesFinalTableRowsAndPageBounds(t *testing.T) {
	r := planning.ReportResult{Type: "spending", From: "2026-01-01", To: "2026-12-31", TotalCost: 987720751}
	for i := 0; i < 365; i++ {
		r.Series = append(r.Series, planning.ReportPoint{Period: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("2006-01-02"), TotalCost: int64(i + 1)})
	}
	r.Series[364].TotalCost = 987654321
	path, text := chartPDF(t, "chart-year", r)
	for _, want := range []string{"Xu hướng chi tiêu", "2026-12-31", "987654321", "Trang 1", "Trang 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("chart or final table row missing %q", want)
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
		t.Fatal("chart displaced table rows without pagination")
	}
}

func TestPDFChartsHandleEmptyZeroAndExtremeFixedPoint(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		r     planning.ReportResult
		wants []string
	}{
		{"empty", planning.ReportResult{Type: "waste", From: "2026-11-01", To: "2026-11-01"}, []string{"Không có dữ liệu dương để vẽ biểu đồ"}},
		{"zero", planning.ReportResult{Type: "spending", Series: []planning.ReportPoint{{Period: "2026-11-01"}}}, []string{"Không có dữ liệu dương để vẽ biểu đồ"}},
		{"maximum", planning.ReportResult{Type: "spending", Series: []planning.ReportPoint{{Period: "2026-11-01", TotalCost: math.MaxInt64}}}, []string{"9223372036854775807"}},
		{"micro", planning.ReportResult{Type: "consumption", Series: []planning.ReportPoint{{Period: "2026-11-01", Quantities: map[string]kernel.Quantity{"mass": 1}}}}, []string{"0.000001", "Đơn vị: g"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			_, text := chartPDF(t, "chart-"+fixture.name, fixture.r)
			for _, want := range fixture.wants {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q in %s", want, text)
				}
			}
		})
	}
}
