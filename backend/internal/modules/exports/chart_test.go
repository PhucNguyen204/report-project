package exports

import (
	"dicho/internal/kernel"
	"dicho/internal/modules/planning"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestChartBucketsKeepEveryDayAndExactTotals(t *testing.T) {
	r := planning.ReportResult{Type: "spending"}
	for day := 0; day < 365; day++ {
		r.Series = append(r.Series, planning.ReportPoint{Period: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, day).Format("2006-01-02"), TotalCost: int64(day + 1)})
	}
	charts, err := buildCharts(r)
	if err != nil || len(charts) != 1 || charts[0].Unit != "VND" || len(charts[0].Bars) != 8 {
		t.Fatalf("missing bounded spending chart: %+v %v", charts, err)
	}
	var totals []int64
	for _, bar := range charts[0].Bars {
		totals = append(totals, bar.Value)
	}
	if !reflect.DeepEqual(totals, []int64{1081, 3197, 5313, 7429, 9545, 11385, 13410, 15435}) || charts[0].Bars[0].First != "2026-01-01" || charts[0].Bars[7].Last != "2026-12-31" {
		t.Fatalf("chart lost or rounded days: %+v", charts[0].Bars)
	}
}

func TestQuantityChartsKeepDimensionsAndMicroPrecisionSeparate(t *testing.T) {
	r := planning.ReportResult{Type: "consumption", Series: []planning.ReportPoint{{Period: "2026-11-01", Quantities: map[string]kernel.Quantity{"mass": 1, "volume": 2_500_000, "count": 3_000_000}}, {Period: "2026-11-02", Quantities: map[string]kernel.Quantity{"mass": 3, "count": 1_000_000}}}}
	charts, err := buildCharts(r)
	if err != nil || len(charts) != 3 {
		t.Fatalf("separate quantity charts missing: %+v %v", charts, err)
	}
	for i, want := range []struct {
		unit   string
		values []int64
	}{{"g", []int64{1, 3}}, {"ml", []int64{2_500_000, 0}}, {"cái", []int64{3_000_000, 1_000_000}}} {
		var values []int64
		for _, bar := range charts[i].Bars {
			values = append(values, bar.Value)
		}
		if charts[i].Unit != want.unit || !reflect.DeepEqual(values, want.values) {
			t.Fatalf("mixed dimensions: %+v", charts)
		}
	}
}

func TestChartRejectsNegativeAndOverflowButKeepsInt64Boundary(t *testing.T) {
	for _, r := range []planning.ReportResult{
		{Type: "spending", Series: []planning.ReportPoint{{TotalCost: -1}}},
		{Type: "waste", Series: []planning.ReportPoint{{Quantities: map[string]kernel.Quantity{"mass": -1}}}},
	} {
		if _, err := buildCharts(r); err == nil {
			t.Fatal("negative chart amount accepted")
		}
	}
	r := planning.ReportResult{Type: "spending"}
	for i := 0; i < 9; i++ {
		r.Series = append(r.Series, planning.ReportPoint{TotalCost: math.MaxInt64})
	}
	if _, err := buildCharts(r); err == nil {
		t.Fatal("bucket overflow accepted")
	}
	r.Series = r.Series[:1]
	charts, err := buildCharts(r)
	if err != nil || len(charts) != 1 || charts[0].Bars[0].Value != math.MaxInt64 {
		t.Fatalf("int64 boundary corrupted: %+v %v", charts, err)
	}
}

func TestChartsRepresentEmptyAndZeroWithoutInventingValues(t *testing.T) {
	for _, r := range []planning.ReportResult{{Type: "spending"}, {Type: "waste"}, {Type: "consumption", Series: []planning.ReportPoint{{Period: "2026-11-01", Quantities: map[string]kernel.Quantity{"mass": 0}}}}} {
		charts, err := buildCharts(r)
		if err != nil || len(charts) != 1 {
			t.Fatalf("empty chart missing %+v %v", charts, err)
		}
		for _, bar := range charts[0].Bars {
			if bar.Value != 0 {
				t.Fatal("invented amount")
			}
		}
	}
}
