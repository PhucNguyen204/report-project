package accounts

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"

	"dicho/internal/platform"
)

func requestVersion(r *http.Request) (int64, error) {
	text := r.Header.Get("If-Match")
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	if text == "" || strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, platform.Invalid("If-Match phải chứa phiên bản hợp lệ")
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 1 {
		return 0, platform.Invalid("If-Match phải chứa phiên bản hợp lệ")
	}
	return value, nil
}
func versionTag(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", `"`+strconv.FormatInt(version, 10)+`"`)
}

func overwrittenFields(ctx context.Context, tx pgx.Tx, table, id string, base, current int64, patch, state map[string]any) ([]string, error) {
	result := []string{}
	if base > current {
		return nil, platform.Conflict()
	}
	if base == current || len(patch) == 0 {
		return result, nil
	}
	desired, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	previous, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE(array_agg(p.key ORDER BY p.key),ARRAY[]::text[]) FROM `+table+` r CROSS JOIN LATERAL jsonb_each($2::jsonb) p WHERE r.id=$1 AND COALESCE((r.field_versions->>p.key)::bigint,1)>$3 AND p.value IS DISTINCT FROM ($4::jsonb->p.key)`, id, desired, base, previous).Scan(&result)
	return result, err
}
