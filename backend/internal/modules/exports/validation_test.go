package exports

import "testing"

func TestExportRangeUsesInclusiveTwelveMonthBoundary(t *testing.T) {
	for _, tc := range []struct {
		to   string
		fail bool
	}{{"2026-12-31", false}, {"2027-01-01", true}} {
		in := Input{From: "2026-01-01", To: tc.to, Format: "csv", ReportType: "spending"}
		_, _, err := validate(&in)
		if (err != nil) != tc.fail {
			t.Errorf("inclusive range ending %s: %v", tc.to, err)
		}
	}
}
