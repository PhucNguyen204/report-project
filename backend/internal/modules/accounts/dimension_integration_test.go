//go:build integration

package accounts

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestFoodRejectsCrossDimensionUnit(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "dimension@example.com")
	err := f.db.Tx(context.Background(), func(tx pgx.Tx) error {
		_, err := f.s.ValidateFood(context.Background(), tx, u["spaceId"].(string), "00000000-0000-7000-8000-000000000021", "00000000-0000-7000-8000-000000000014")
		return err
	})
	if err == nil {
		t.Fatal("carrot mass converted to millilitres")
	}
}
