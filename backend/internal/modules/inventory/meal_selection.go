package inventory

import (
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/hex"
	"sort"
	"strings"
)

func selectedLotIDs(line platform.Consumption) ([]string, error) {
	if len(line.LotIDs) == 0 {
		return nil, nil
	}
	if line.LotID != "" || len(line.LotIDs) > 100 {
		return nil, platform.Invalid("Danh sách lô không hợp lệ")
	}
	ids := make([]string, len(line.LotIDs))
	seen := make(map[string]bool, len(ids))
	for i, id := range line.LotIDs {
		id, err := canonicalLotID(id)
		if err != nil || seen[id] {
			return nil, platform.Invalid("Danh sách lô không hợp lệ")
		}
		ids[i], seen[id] = id, true
	}
	return ids, nil
}

func canonicalLotID(id string) (string, error) {
	id = strings.ToLower(id)
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return "", platform.Invalid("Mã lô không hợp lệ")
	}
	if raw, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err != nil || len(raw) != 16 {
		return "", platform.Invalid("Mã lô không hợp lệ")
	}
	return id, nil
}

func allocateSelectedLots(lots []AllocationLot, needed kernel.Quantity) ([]Allocation, error) {
	sorted := append([]AllocationLot(nil), lots...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ExpiresOn.Equal(sorted[j].ExpiresOn) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].ExpiresOn.Before(sorted[j].ExpiresOn)
	})
	result := make([]Allocation, 0, len(sorted))
	for _, lot := range sorted {
		if lot.Available <= 0 {
			continue
		}
		take := min(lot.Available, needed)
		result = append(result, Allocation{LotID: lot.ID, Quantity: take})
		needed -= take
		if needed == 0 {
			return result, nil
		}
	}
	return nil, platform.Err(422, 2003, "Số lượng vượt lượng còn lại")
}
