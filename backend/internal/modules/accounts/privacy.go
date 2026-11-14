package accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RegisterPrivacy(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /it4788/v1/me", platform.Handle(s.deleteAccount))
}
func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Password string `json:"password"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	uid := platform.Current(r).UserID
	ok, err := s.passwordMatches(r.Context(), uid, in.Password)
	if err != nil {
		return err
	}
	if !ok {
		return platform.Invalid("Mật khẩu không đúng")
	}
	rows, err := s.db.Pool.Query(r.Context(), "SELECT space_id::text FROM household.space_members WHERE user_id=$1 AND space_kind='family' ORDER BY space_id", uid)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for rows.Next() {
		var sid string
		if err = rows.Scan(&sid); err != nil {
			rows.Close()
			return err
		}
		known[sid] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		ordered := []string{}
		for sid := range known {
			ordered = append(ordered, sid)
		}
		sort.Strings(ordered)
		for _, sid := range ordered {
			if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtext($1))", sid); err != nil {
				return err
			}
		}
		var hash string
		if err := tx.QueryRow(r.Context(), "SELECT password_hash FROM identity.users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", uid).Scan(&hash); err != nil {
			return err
		}
		if _, err := s.db.LockActor(r.Context(), tx, uid); err != nil {
			return err
		}
		if !verifyPassword(hash, in.Password) {
			return platform.Conflict()
		}
		rows, err := tx.Query(r.Context(), "SELECT space_id::text,role FROM household.space_members WHERE user_id=$1 AND space_kind='family' ORDER BY space_id FOR UPDATE", uid)
		if err != nil {
			return err
		}
		type family struct{ sid, role string }
		families := []family{}
		for rows.Next() {
			var f family
			if err = rows.Scan(&f.sid, &f.role); err != nil {
				rows.Close()
				return err
			}
			families = append(families, f)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, f := range families {
			if !known[f.sid] {
				return platform.Conflict()
			}
			if f.role == "leader" {
				var n int
				if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM household.space_members WHERE space_id=$1", f.sid).Scan(&n); err != nil {
					return err
				}
				if n > 1 {
					return platform.Rule("Hãy chuyển quyền trưởng nhóm trước khi xóa tài khoản")
				}
			}
		}
		for _, f := range families {
			if _, err = tx.Exec(r.Context(), "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", f.sid, uid); err != nil {
				return err
			}
			if s.db.MemberLeave != nil {
				if err = s.db.MemberLeave(r.Context(), tx, f.sid, uid); err != nil {
					return err
				}
			}
			if f.role == "leader" {
				if _, err = tx.Exec(r.Context(), "UPDATE household.spaces SET dissolved_at=$2 WHERE id=$1", f.sid, s.db.Clock.Now()); err != nil {
					return err
				}
				if _, err = tx.Exec(r.Context(), "UPDATE household.invite_codes SET revoked_at=$2 WHERE space_id=$1 AND revoked_at IS NULL", f.sid, s.db.Clock.Now()); err != nil {
					return err
				}
				if err = s.db.Enqueue(r.Context(), tx, "space.purge", f.sid, map[string]any{"spaceId": f.sid}, s.db.Clock.Now()); err != nil {
					return err
				}
			}
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.users SET deleted_at=$2,token_version=token_version+1 WHERE id=$1", uid, s.db.Clock.Now()); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET revoked_at=$2,revoke_reason='account_deleted' WHERE user_id=$1 AND revoked_at IS NULL", uid, s.db.Clock.Now()); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "DELETE FROM identity.devices WHERE user_id=$1", uid); err != nil {
			return err
		}
		return s.db.Enqueue(r.Context(), tx, "account.purge", uid, map[string]any{"userId": uid}, s.db.Clock.Now().Add(30*24*time.Hour))
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"deleted": true})
	return nil
}
func (s *Service) HandleAccountPurge(ctx context.Context, e platform.Event) error {
	var in struct {
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		return err
	}
	if in.UserID == "" {
		return platform.Invalid("Thiếu tài khoản cần xóa")
	}
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		var deleted, anonymized *time.Time
		if err := tx.QueryRow(ctx, "SELECT deleted_at,anonymized_at FROM identity.users WHERE id=$1 FOR UPDATE", in.UserID).Scan(&deleted, &anonymized); err != nil {
			return err
		}
		if anonymized != nil {
			return nil
		}
		if deleted == nil || s.db.Clock.Now().Before(deleted.Add(30*24*time.Hour)) {
			return platform.Rule("Chưa tới hạn ẩn danh tài khoản")
		}
		rows, err := tx.Query(ctx, "SELECT space_id::text FROM household.space_members WHERE user_id=$1 AND space_kind='personal' ORDER BY space_id", in.UserID)
		if err != nil {
			return err
		}
		spaces := []string{}
		for rows.Next() {
			var sid string
			if err = rows.Scan(&sid); err != nil {
				rows.Close()
				return err
			}
			spaces = append(spaces, sid)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, sid := range spaces {
			if _, err = tx.Exec(ctx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", sid, in.UserID); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, "UPDATE household.spaces SET dissolved_at=COALESCE(dissolved_at,$2) WHERE id=$1", sid, s.db.Clock.Now()); err != nil {
				return err
			}
			if err = s.db.Enqueue(ctx, tx, "space.purge", sid, map[string]any{"spaceId": sid}, s.db.Clock.Now()); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, "DELETE FROM identity.otp_challenges WHERE user_id=$1", in.UserID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM identity.refresh_sessions WHERE user_id=$1", in.UserID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE identity.users SET email=$2,full_name='Người dùng đã xóa',password_hash='deleted',status='disabled',avatar_object_id=NULL,last_space_id=NULL,anonymized_at=$3 WHERE id=$1", in.UserID, "deleted+"+in.UserID+"@deleted.invalid", s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Enqueue(ctx, tx, "account.anonymized", in.UserID, map[string]any{"userId": in.UserID}, s.db.Clock.Now())
	})
}
