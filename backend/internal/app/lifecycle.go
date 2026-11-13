package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"dicho/internal/modules/accounts"
	"dicho/internal/modules/exports"
	"dicho/internal/modules/inventory"
	"dicho/internal/modules/media"
	"dicho/internal/modules/notification"
	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

type Dispatcher struct {
	DB            *platform.DB
	Accounts      *accounts.Service
	Inventory     *inventory.Service
	Planning      *planning.Service
	Media         *media.Service
	Exports       *exports.Service
	Notifications *notification.Service
}

func (d *Dispatcher) Handle(ctx context.Context, e platform.Event) error {
	switch e.Event {
	case "email", "notification":
		return d.Notifications.Handle(ctx, e)
	case "assignment.digest":
		return d.Inventory.HandleAssignmentDigest(ctx, e)
	case "report.export", "export.delete":
		return d.Exports.Handle(ctx, e)
	case "media.delete":
		return d.Media.Handle(ctx, e)
	case "account.purge":
		var in struct {
			UserID string `json:"userId"`
		}
		if err := decodeLifecycle(e.Payload, &in); err != nil {
			return err
		}
		if !lifecycleID(in.UserID) {
			return platform.Invalid("Thiếu tài khoản cần xóa")
		}
		return d.Accounts.HandleAccountPurge(ctx, e)
	case "account.anonymized":
		var in struct {
			UserID string `json:"userId"`
		}
		if err := decodeLifecycle(e.Payload, &in); err != nil {
			return err
		}
		if !lifecycleID(in.UserID) {
			return platform.Invalid("Thiếu tài khoản cần ẩn danh")
		}
		return d.DB.Tx(ctx, func(tx pgx.Tx) error {
			joined := platform.WithTransaction(ctx, tx)
			if err := d.Media.Handle(joined, e); err != nil {
				return err
			}
			return d.Notifications.PurgeUser(joined, tx, in.UserID)
		})
	case "config.changed":
		var in struct {
			Values map[string]json.RawMessage `json:"values"`
		}
		if err := decodeLifecycle(e.Payload, &in); err != nil {
			return err
		}
		if len(in.Values) == 0 {
			return platform.Invalid("Thiếu tham số đã thay đổi")
		}
		// Configuration consumers also read PostgreSQL directly.
		return nil
	case "settings.changed":
		var in struct {
			UserID string `json:"userId"`
		}
		if err := decodeLifecycle(e.Payload, &in); err != nil {
			return err
		}
		if !lifecycleID(in.UserID) {
			return platform.Invalid("Thiếu tài khoản thay đổi thiết lập")
		}
		// Settings are read from PostgreSQL on every request; no process cache needs invalidation.
		return nil
	case "space.copy":
		return d.copySpace(ctx, e)
	case "space.purge":
		return d.purgeSpace(ctx, e)
	case "food.merged":
		return d.remapFood(ctx, e)
	default:
		return fmt.Errorf("unsupported worker event %q", e.Event)
	}
}

func decodeLifecycle(raw json.RawMessage, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return platform.Invalid("Dữ liệu tác vụ không hợp lệ")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return platform.Invalid("Dữ liệu tác vụ không hợp lệ")
	}
	return nil
}
func lifecycleID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil
}
func (d *Dispatcher) once(ctx context.Context, e platform.Event, apply func(pgx.Tx) error) error {
	if strings.TrimSpace(e.Key) == "" {
		return platform.Invalid("Thiếu mã tác vụ")
	}
	return d.DB.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "worker:"+e.Key); err != nil {
			return err
		}
		var kind string
		err := tx.QueryRow(ctx, `SELECT kind FROM platform.worker_receipts WHERE event_key=$1`, e.Key).Scan(&kind)
		if err == nil {
			if kind != e.Event {
				return platform.Conflict()
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err = apply(tx); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO platform.worker_receipts(event_key,kind,created_at) VALUES($1,$2,$3)`, e.Key, e.Event, d.DB.Clock.Now())
		return err
	})
}
func lockLifecycleSpaces(ctx context.Context, tx pgx.Tx, ids ...string) error {
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, id); err != nil {
			return err
		}
	}
	return nil
}
func (d *Dispatcher) copySpace(ctx context.Context, e platform.Event) error {
	var in struct {
		Source  string `json:"sourceSpaceId"`
		Target  string `json:"targetSpaceId"`
		User    string `json:"userId"`
		Lists   bool   `json:"lists"`
		Lots    bool   `json:"lots"`
		Recipes bool   `json:"recipes"`
	}
	if err := decodeLifecycle(e.Payload, &in); err != nil {
		return err
	}
	if !lifecycleID(in.Source) || !lifecycleID(in.Target) || !lifecycleID(in.User) || in.Source == in.Target || (!in.Lists && !in.Lots && !in.Recipes) {
		return platform.Invalid("Dữ liệu sao chép không gian không hợp lệ")
	}
	return d.once(ctx, e, func(tx pgx.Tx) error {
		if err := lockLifecycleSpaces(ctx, tx, in.Source, in.Target); err != nil {
			return err
		}
		var source string
		err := tx.QueryRow(ctx, `SELECT h.id::text FROM household.spaces h JOIN household.space_members m ON m.space_id=h.id JOIN identity.users u ON u.id=m.user_id WHERE h.id=$1 AND h.kind='personal' AND h.created_by=$2 AND m.user_id=$2 AND u.status='active' AND u.system_role='user' AND u.deleted_at IS NULL AND h.dissolved_at IS NULL FOR SHARE OF h,m,u`, in.Source, in.User).Scan(&source)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		var target string
		err = tx.QueryRow(ctx, `SELECT h.id::text FROM household.spaces h JOIN household.space_members m ON m.space_id=h.id WHERE h.id=$1 AND h.kind='family' AND h.dissolved_at IS NULL AND m.user_id=$2 FOR SHARE OF h,m`, in.Target, in.User).Scan(&target)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		foods, err := d.Accounts.ClonePrivateFoods(ctx, tx, source, target)
		if err != nil {
			return err
		}
		if err = d.Inventory.CopySpace(ctx, tx, source, target, in.User, in.Lists, in.Lots, foods); err != nil {
			return err
		}
		if in.Recipes {
			return d.Planning.CopySpace(ctx, tx, source, target, in.User, foods)
		}
		return nil
	})
}
func (d *Dispatcher) purgeSpace(ctx context.Context, e platform.Event) error {
	var in struct {
		Space string `json:"spaceId"`
	}
	if err := decodeLifecycle(e.Payload, &in); err != nil {
		return err
	}
	if !lifecycleID(in.Space) {
		return platform.Invalid("Thiếu không gian cần xóa")
	}
	return d.once(ctx, e, func(tx pgx.Tx) error {
		if err := lockLifecycleSpaces(ctx, tx, in.Space); err != nil {
			return err
		}
		var dissolved bool
		err := tx.QueryRow(ctx, `SELECT dissolved_at IS NOT NULL FROM household.spaces WHERE id=$1 FOR UPDATE`, in.Space).Scan(&dissolved)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !dissolved {
			return platform.Rule("Chỉ xóa không gian đã giải tán")
		}
		if err = d.Media.PurgeSpace(ctx, tx, in.Space); err != nil {
			return err
		}
		if err = d.Exports.PurgeSpace(ctx, tx, in.Space); err != nil {
			return err
		}
		if err = d.Planning.PurgeSpace(ctx, tx, in.Space); err != nil {
			return err
		}
		if err = d.Inventory.PurgeSpace(ctx, tx, in.Space); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM platform.activities WHERE space_id=$1`, in.Space); err != nil {
			return err
		}
		return d.Accounts.PurgeSpace(ctx, tx, in.Space)
	})
}
func (d *Dispatcher) remapFood(ctx context.Context, e platform.Event) error {
	var in struct {
		Source string `json:"sourceFoodId"`
		Target string `json:"targetFoodId"`
	}
	if err := decodeLifecycle(e.Payload, &in); err != nil {
		return err
	}
	if !lifecycleID(in.Source) || !lifecycleID(in.Target) || in.Source == in.Target {
		return platform.Invalid("Dữ liệu gộp thực phẩm không hợp lệ")
	}
	return d.once(ctx, e, func(tx pgx.Tx) error {
		var target *string
		if err := tx.QueryRow(ctx, `SELECT merged_into::text FROM catalog.foods WHERE id=$1 FOR SHARE`, in.Source).Scan(&target); errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		} else if err != nil {
			return err
		}
		if target == nil || !strings.EqualFold(*target, in.Target) {
			return platform.Rule("Ánh xạ thực phẩm chưa được duyệt")
		}
		if err := d.Inventory.RemapFood(ctx, tx, in.Source, in.Target); err != nil {
			return err
		}
		return d.Planning.RemapFood(ctx, tx, in.Source, in.Target)
	})
}
