package inventory

import (
	"dicho/internal/kernel"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func FuzzAllocateConservesStock(f *testing.F) {
	f.Add([]byte{1, 4, 2, 5, 3, 6}, uint8(6), uint8(0))
	f.Add([]byte{255, 0, 255, 8}, uint8(10), uint8(0))
	f.Add([]byte{200, 0}, uint8(1), uint8(1))
	f.Add([]byte{}, uint8(0), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, demand, selection uint8) {
		if len(data) > 32 {
			return
		}
		today := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
		lots := make([]AllocationLot, 0, 8)
		for i := 0; i+1 < len(data) && len(lots) < 8; i += 2 {
			lots = append(lots, AllocationLot{
				ID: fmt.Sprintf("lot-%d", i/2), Available: kernel.Quantity(data[i]) * 97,
				ExpiresOn: today.AddDate(0, 0, int(data[i+1]%9)-4),
			})
		}
		before := append([]AllocationLot{}, lots...)
		explicit := ""
		if selection%3 == 1 && len(lots) > 0 {
			explicit = lots[int(selection)%len(lots)].ID
		} else if selection%3 == 2 {
			explicit = "missing"
		}
		eligible := make(map[string]AllocationLot)
		var supply kernel.Quantity
		for _, lot := range lots {
			if lot.Available > 0 && ((explicit == "" && !lot.ExpiresOn.Before(today)) || explicit == lot.ID) {
				eligible[lot.ID] = lot
				supply += lot.Available
			}
		}
		needed := kernel.Quantity(int(demand)+1) * 97
		got, err := Allocate(lots, needed, today, explicit)
		if !reflect.DeepEqual(lots, before) {
			t.Fatal("allocation mutated input lots")
		}
		if supply < needed {
			if err == nil || len(got) != 0 {
				t.Fatalf("insufficient stock produced partial success: supply=%d needed=%d got=%v err=%v", supply, needed, got, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("sufficient stock rejected: %v", err)
		}
		used := make(map[string]kernel.Quantity)
		var total kernel.Quantity
		for _, allocation := range got {
			lot, ok := eligible[allocation.LotID]
			if !ok || used[allocation.LotID] != 0 || allocation.Quantity <= 0 || allocation.Quantity > lot.Available {
				t.Fatalf("invalid allocation: %+v", allocation)
			}
			used[allocation.LotID] = allocation.Quantity
			total += allocation.Quantity
		}
		if total != needed {
			t.Fatalf("quantity not conserved: got=%d needed=%d", total, needed)
		}
		for id := range used {
			later := eligible[id]
			for earlierID, earlier := range eligible {
				precedes := earlier.ExpiresOn.Before(later.ExpiresOn) || (earlier.ExpiresOn.Equal(later.ExpiresOn) && earlierID < id)
				if precedes && used[earlierID] != earlier.Available {
					t.Fatalf("used %s before exhausting earlier %s", id, earlierID)
				}
			}
		}
	})
}
