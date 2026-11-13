package planning

import (
	"dicho/internal/kernel"
	"reflect"
	"testing"
	"time"
)

func FuzzMissingNeverReusesSupply(f *testing.F) {
	f.Add(uint8(10), uint8(15), uint8(20), uint8(0), uint8(5), uint8(0), false)
	f.Add(uint8(1), uint8(1), uint8(255), uint8(5), uint8(0), uint8(1), false)
	f.Add(uint8(255), uint8(255), uint8(0), uint8(0), uint8(0), uint8(2), true)
	f.Fuzz(func(t *testing.T, a, b, stock, dayA, dayB, mode uint8, optional bool) {
		today := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
		demands := []Demand{
			{FoodID: "food", UnitID: "g", Dimension: "mass", Quantity: kernel.Quantity(int(a)+1) * 10_000, On: today.AddDate(0, 0, int(dayA%6))},
			{FoodID: "food", UnitID: "g", Dimension: "mass", Quantity: kernel.Quantity(int(b)+1) * 10_000, On: today.AddDate(0, 0, int(dayB%6)), Optional: optional},
		}
		available, expires := today, today.AddDate(0, 0, 5)
		if mode%3 == 1 {
			available = today.AddDate(0, 0, 6)
		} else if mode%3 == 2 {
			expires = today.AddDate(0, 0, -1)
		}
		supply := []Supply{
			{FoodID: "food", Dimension: "mass", Quantity: kernel.Quantity(stock) * 10_000, AvailableOn: available, ExpiresOn: expires},
			{FoodID: "food", Dimension: "volume", Quantity: 100_000_000},
			{FoodID: "other", Dimension: "mass", Quantity: 100_000_000},
		}
		beforeD, beforeS := append([]Demand(nil), demands...), append([]Supply(nil), supply...)
		got, err := Missing(demands, supply)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(demands, beforeD) || !reflect.DeepEqual(supply, beforeS) {
			t.Fatal("missing calculation mutated inputs")
		}
		required := demands[0].Quantity
		if !optional {
			required += demands[1].Quantity
		}
		eligible := kernel.Quantity(0)
		if mode%3 == 0 {
			eligible = kernel.Quantity(stock) * 10_000
		}
		want := max(required-eligible, 0)
		var total kernel.Quantity
		for i, need := range got {
			if need.Quantity <= 0 || need.Optional || need.FoodID != "food" || need.UnitID != "g" || need.Dimension != "mass" {
				t.Fatalf("invalid shortage: %+v", need)
			}
			if i > 0 && got[i-1].On.After(need.On) {
				t.Fatal("shortages not in cooking-date order")
			}
			total += need.Quantity
		}
		if total != want {
			t.Fatalf("stock reused or ineligible supply used: got=%d want=%d", total, want)
		}
	})
}

func FuzzMealDatesRespectCivilHorizon(f *testing.F) {
	f.Add("2026-11-13", int16(0), uint8(0))
	f.Add("2024-02-29", int16(7), uint8(0))
	f.Add("2026-02-29", int16(8), uint8(0))
	f.Add("", int16(-1), uint8(53))
	f.Fuzz(func(t *testing.T, text string, offset int16, h uint8) {
		if len(text) <= 64 {
			if parsed, err := parseDate(text); err == nil && parsed.Format("2006-01-02") != text {
				t.Fatalf("noncanonical date accepted: %q", text)
			}
		}
		zone := time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
		today := time.Date(2026, 11, 13, 15, 0, 0, 0, zone)
		on := time.Date(2026, 11, 13, 0, 30, 0, 0, zone).AddDate(0, 0, int(offset))
		horizon := 7 + int(h%54)
		err := validateMealDate(today, on, horizon)
		valid := offset >= 0 && int(offset) <= horizon
		if (err == nil) != valid {
			t.Fatalf("civil horizon: offset=%d horizon=%d err=%v", offset, horizon, err)
		}
	})
}
