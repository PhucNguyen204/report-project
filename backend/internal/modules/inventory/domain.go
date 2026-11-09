package inventory

import (
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"math/big"
	"sort"
	"time"
)

type AllocationLot struct {
	ID        string
	ExpiresOn time.Time
	Available kernel.Quantity
}
type Allocation struct {
	LotID    string
	Quantity kernel.Quantity
}

func Allocate(lots []AllocationLot, needed kernel.Quantity, today time.Time, explicit string) ([]Allocation, error) {
	if needed <= 0 {
		return nil, platform.Invalid("Số lượng phải dương")
	}
	sorted := append([]AllocationLot(nil), lots...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ExpiresOn.Equal(sorted[j].ExpiresOn) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].ExpiresOn.Before(sorted[j].ExpiresOn)
	})
	result := make([]Allocation, 0)
	for _, lot := range sorted {
		if lot.Available <= 0 || (explicit != "" && lot.ID != explicit) || (explicit == "" && lot.ExpiresOn.Before(today)) {
			continue
		}
		take := lot.Available
		if take > needed {
			take = needed
		}
		result = append(result, Allocation{lot.ID, take})
		needed -= take
		if needed == 0 {
			return result, nil
		}
	}
	return nil, platform.Err(422, 2003, "Số lượng vượt lượng còn lại")
}

func ExpiryState(expires, today time.Time, n int) string {
	if expires.Format("2006-01-02") < today.Format("2006-01-02") {
		return "expired"
	}
	if expires.Format("2006-01-02") <= today.AddDate(0, 0, n).Format("2006-01-02") {
		return "expiring"
	}
	return "fresh"
}

func totalCost(amount kernel.Quantity, price int64) (int64, error) {
	if amount <= 0 || price < 0 {
		return 0, platform.Invalid("Giá hoặc số lượng không hợp lệ")
	}
	n := new(big.Int).Mul(big.NewInt(int64(amount)), big.NewInt(price))
	n.Add(n, big.NewInt(kernel.Scale/2))
	n.Quo(n, big.NewInt(kernel.Scale))
	if !n.IsInt64() {
		return 0, platform.Invalid("Giá trị vượt giới hạn")
	}
	return n.Int64(), nil
}

func validQuantity(q kernel.Quantity) error {
	if q <= 0 || !q.TwoDecimals() || q > kernel.Quantity(9_999_999_999_990_000) {
		return platform.Invalid("Số lượng phải dương và có tối đa hai chữ số thập phân")
	}
	return nil
}
func validLocation(s string) bool {
	switch s {
	case "chill", "freezer", "crisper", "door", "pantry_shelf":
		return true
	}
	return false
}
