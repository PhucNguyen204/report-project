//go:build integration

package accounts

import (
	"context"
	"testing"
)

func TestNullProfilePatchCannotBumpVersionOrPublishActivity(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "null-profile@example.test")
	var before, after, activities int
	ctx := context.Background()
	if err := f.db.Pool.QueryRow(ctx, "SELECT version FROM identity.users WHERE id=$1", u["userId"]).Scan(&before); err != nil {
		t.Fatal(err)
	}
	status, _ := f.call(t, "PATCH", "/it4788/v1/me", nil, u["accessToken"].(string))
	if status != 400 {
		t.Fatalf("null profile patch returned%d", status)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT (SELECT version FROM identity.users WHERE id=$1),(SELECT count(*) FROM platform.activities WHERE actor_id=$1 AND kind='member.updated')`, u["userId"]).Scan(&after, &activities); err != nil || before != after || activities != 0 {
		t.Fatalf("null mutation: version%d -> %d activities%d err%v", before, after, activities, err)
	}
}
