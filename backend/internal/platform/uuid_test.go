package platform

import "testing"

func TestUUID7AcceptsCanonicalClientIDsAndRejectsOtherVersions(t *testing.T) {
	for _, id := range []string{ID(), "00000000-0000-7000-8000-000000000099", "00000000-0000-7000-B000-000000000099"} {
		if !UUID7(id) {
			t.Fatalf("valid UUIDv7 rejected%s", id)
		}
	}
	for _, id := range []string{"", "bad", "00000000-0000-4000-8000-000000000099", "00000000-0000-7000-c000-000000000099", "00000000-0000-7000-8000-00000000009x"} {
		if UUID7(id) {
			t.Fatalf("invalid UUIDv7 accepted%s", id)
		}
	}
}
