package platform

import "strings"

func UUID7(id string) bool {
	if !uuidKey(id) || id[14] != '7' {
		return false
	}
	return strings.ContainsRune("89ab", rune(strings.ToLower(id)[19]))
}
