//go:build integration

package accounts

import "testing"

func TestForgotPasswordDoesNotRevealKnownEmailThroughCooldown(t *testing.T) {
	f := setupAuth(t)
	f.activate(t, "forgot-private@example.com")
	for range 2 {
		for _, email := range []string{"forgot-private@example.com", "unknown-private@example.com"} {
			status, body := f.call(t, "POST", "/it4788/v1/auth/password/forgot", map[string]any{"email": email}, "")
			if status != 200 || body["data"].(map[string]any)["accepted"] != true {
				t.Fatalf("email disclosed via status %d %+v", status, body)
			}
		}
	}
}
