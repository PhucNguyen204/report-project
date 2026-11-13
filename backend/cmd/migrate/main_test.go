package main

import (
	"context"
	"strings"
	"testing"
)

func TestMigratorRejectsMissingAndMalformedDatabaseURLWithoutSecrets(t *testing.T) {
	for _, dsn := range []string{"", "postgres://%SECRET"} {
		if err := run(context.Background(), func(string) string { return dsn }); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("invalid database URL leaked or succeeded: %v", err)
		}
	}
}
