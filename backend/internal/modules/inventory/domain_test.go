package inventory

import (
	"dicho/internal/kernel"
	"testing"
	"time"
)

func qty(s string) kernel.Quantity {
	q, err := kernel.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return q
}
func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestAllocateFEFOExcludesExpiredAndPreservesOrder(t *testing.T) {
	lots := []AllocationLot{{ID: "b", ExpiresOn: day("2026-11-11"), Available: qty("0.5")}, {ID: "expired", ExpiresOn: day("2026-11-08"), Available: qty("99")}, {ID: "a", ExpiresOn: day("2026-11-10"), Available: qty("0.3")}}
	got, err := Allocate(lots, qty("0.6"), day("2026-11-09"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].LotID != "a" || got[0].Quantity != qty("0.3") || got[1].LotID != "b" || got[1].Quantity != qty("0.3") {
		t.Fatalf("bad allocation: %+v", got)
	}
	if lots[0].Available != qty("0.5") {
		t.Fatal("mutated caller lots")
	}
}

func TestAllocateExplicitLotAndInsufficientStock(t *testing.T) {
	lots := []AllocationLot{{ID: "expired", ExpiresOn: day("2026-11-08"), Available: qty("1")}}
	if _, err := Allocate(lots, qty("0.2"), day("2026-11-09"), ""); err == nil {
		t.Fatal("automatically consumed expired lot")
	}
	got, err := Allocate(lots, qty("0.2"), day("2026-11-09"), "expired")
	if err != nil || len(got) != 1 || got[0].Quantity != qty("0.2") {
		t.Fatalf("explicit expired selection: %+v %v", got, err)
	}
	for _, amount := range []string{"0", "-1", "2"} {
		if _, err := Allocate(lots, qty(amount), day("2026-11-09"), "expired"); err == nil {
			t.Fatalf("accepted %s", amount)
		}
	}
}

func TestExpiryStateBoundaries(t *testing.T) {
	for _, tc := range []struct{ expiry, want string }{{"2026-11-08", "expired"}, {"2026-11-09", "expiring"}, {"2026-11-12", "expiring"}, {"2026-11-13", "fresh"}} {
		if got := ExpiryState(day(tc.expiry), day("2026-11-09"), 3); got != tc.want {
			t.Fatalf("%s: %s", tc.expiry, got)
		}
	}
}

func TestCostHalfUpAndOverflow(t *testing.T) {
	got, err := totalCost(qty("0.5"), 3)
	if err != nil || got != 2 {
		t.Fatalf("half-up got %d %v", got, err)
	}
	if _, err := totalCost(qty("9999999999.99"), 9223372036854775807); err == nil {
		t.Fatal("overflow accepted")
	}
}
