package accounts

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"dicho/internal/kernel"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RegisterAdmin(mux *http.ServeMux) {
	mux.HandleFunc("GET /it4788/v1/admin/configs", platform.Handle(s.getConfigs))
	mux.HandleFunc("PUT /it4788/v1/admin/configs", platform.Handle(s.setConfigs))
	mux.HandleFunc("GET /it4788/v1/admin/audit-logs", platform.Handle(s.auditLogs))
	mux.HandleFunc("GET /it4788/v1/admin/users", platform.Handle(s.adminUsers))
	mux.HandleFunc("PATCH /it4788/v1/admin/users/{id}", platform.Handle(s.adminUser))
}
func validateConfig(key string, value json.RawMessage) error {
	bounds := map[string][2]int{"reminder.defaultDays": {1, 7}, "reminder.maxLotsPerNotice": {1, 10}, "group.maxMembers": {2, 30}, "invite.ttlHours": {1, 168}, "otp.ttlMinutes": {2, 15}, "suggest.maxResults": {3, 30}, "mealplan.horizonDays": {7, 60}}
	if b, ok := bounds[key]; ok {
		var n int
		if json.Unmarshal(value, &n) != nil || n < b[0] || n > b[1] {
			return platform.Invalid("Tham số ngoài miền cho phép")
		}
		return nil
	}
	switch key {
	case "reminder.runAt":
		var text string
		if json.Unmarshal(value, &text) != nil {
			return platform.Invalid("Giờ nhắc không hợp lệ")
		}
		parsed, err := time.Parse("15:04", text)
		if err != nil || parsed.Hour() < 5 || parsed.Hour() > 21 || parsed.Hour() == 21 && parsed.Minute() != 0 {
			return platform.Invalid("Giờ nhắc phải từ 05:00 đến 21:00")
		}
	case "suggest.theta":
		var q kernel.Quantity
		if json.Unmarshal(value, &q) != nil || q < 300000 || q > 1000000 {
			return platform.Invalid("Ngưỡng gợi ý không hợp lệ")
		}
	case "suggest.weights":
		var weights map[string]kernel.Quantity
		if json.Unmarshal(value, &weights) != nil || len(weights) != 3 {
			return platform.Invalid("Trọng số không hợp lệ")
		}
		sum := kernel.Quantity(0)
		for _, key := range []string{"r", "e", "f"} {
			v, ok := weights[key]
			if !ok || v < 0 || v > 1000000 {
				return platform.Invalid("Trọng số không hợp lệ")
			}
			sum += v
		}
		if sum != 1000000 {
			return platform.Invalid("Tổng trọng số phải bằng 1")
		}
	default:
		return platform.Invalid("Tham số không tồn tại")
	}
	return nil
}
func (s *Service) getConfigs(w http.ResponseWriter, r *http.Request) error {
	if err := platform.Admin(r); err != nil {
		return err
	}
	rows, err := s.adminQuery(r.Context(), platform.Current(r).UserID, "SELECT key,value FROM admin.system_configs ORDER BY key")
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"items": rows})
	return nil
}
func (s *Service) setConfigs(w http.ResponseWriter, r *http.Request) error {
	if err := platform.Admin(r); err != nil {
		return err
	}
	var in struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if len(in.Values) == 0 {
		return platform.Invalid("Cần ít nhất một tham số")
	}
	for k, v := range in.Values {
		if err := validateConfig(k, v); err != nil {
			return err
		}
	}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtext('system_configs'))"); err != nil {
			return err
		}
		if err := s.lockAdministrator(r.Context(), tx, platform.Current(r).UserID); err != nil {
			return err
		}
		old := map[string]json.RawMessage{}
		for k, v := range in.Values {
			var prev json.RawMessage
			if err := tx.QueryRow(r.Context(), "SELECT value FROM admin.system_configs WHERE key=$1 FOR UPDATE", k).Scan(&prev); err != nil {
				return err
			}
			old[k] = prev
			if _, err := tx.Exec(r.Context(), "UPDATE admin.system_configs SET value=$2,updated_by=$3,updated_at=$4 WHERE key=$1", k, []byte(v), platform.Current(r).UserID, s.db.Clock.Now()); err != nil {
				return err
			}
		}
		if err := s.audit(r.Context(), tx, platform.Current(r).UserID, "config.update", "configs", old, in.Values); err != nil {
			return err
		}
		return s.db.Enqueue(r.Context(), tx, "config.changed", platform.ID(), map[string]any{"values": in.Values}, s.db.Clock.Now())
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"updated": len(in.Values)})
	return nil
}
func (s *Service) auditLogs(w http.ResponseWriter, r *http.Request) error {
	if err := platform.Admin(r); err != nil {
		return err
	}
	page, err := platform.ReadPage(r, platform.PageTime, platform.PageUUID)
	if err != nil {
		return err
	}
	items, err := s.adminQuery(r.Context(), platform.Current(r).UserID, `SELECT id::text,actor_id::text AS "actorId",action,entity_id AS "entityId",
 CASE WHEN jsonb_typeof(old_value)='object' THEN old_value-'field_versions' ELSE old_value END AS "oldValue",
 CASE WHEN jsonb_typeof(new_value)='object' THEN new_value-'field_versions' ELSE new_value END AS "newValue",
 created_at AS "createdAt" FROM admin.audit_logs
 WHERE ($1='' OR (created_at,id)<(NULLIF($1,'')::timestamptz,NULLIF($2,'')::uuid))
 ORDER BY created_at DESC,id DESC LIMIT $3`, page.After[0], page.After[1], page.Limit+1)
	if err != nil {
		return err
	}
	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		last := items[len(items)-1]
		next = page.Next(last["createdAt"].(time.Time).Format(time.RFC3339Nano), last["id"].(string))
	}
	platform.WritePage(w, map[string]any{"items": items}, next, s.db.Clock.Now())
	return nil
}
func (s *Service) adminUsers(w http.ResponseWriter, r *http.Request) error {
	if err := platform.Admin(r); err != nil {
		return err
	}
	page, err := platform.ReadPage(r, platform.PageTime, platform.PageUUID)
	if err != nil {
		return err
	}
	items, err := s.adminQuery(r.Context(), platform.Current(r).UserID, `SELECT id::text,email,version,full_name AS "fullName",system_role AS "systemRole",status,last_login_at AS "lastLoginAt",lock_reason AS "lockReason",created_at AS "__pageCreatedAt"
 FROM identity.users WHERE deleted_at IS NULL AND ($1='' OR email::text ILIKE '%'||$1||'%' OR full_name ILIKE '%'||$1||'%')
 AND ($2='' OR (created_at,id)<(NULLIF($2,'')::timestamptz,NULLIF($3,'')::uuid))
 ORDER BY created_at DESC,id DESC LIMIT $4`, r.URL.Query().Get("q"), page.After[0], page.After[1], page.Limit+1)
	if err != nil {
		return err
	}
	next := ""
	if len(items) > page.Limit {
		items = items[:page.Limit]
		last := items[len(items)-1]
		next = page.Next(last["__pageCreatedAt"].(time.Time).Format(time.RFC3339Nano), last["id"].(string))
	}
	for _, item := range items {
		delete(item, "__pageCreatedAt")
	}
	platform.WritePage(w, map[string]any{"items": items}, next, s.db.Clock.Now())
	return nil
}
func (s *Service) adminUser(w http.ResponseWriter, r *http.Request) error {
	if err := platform.Admin(r); err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
		Role   string `json:"systemRole"`
		Reason string `json:"lockReason"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.Status != "" && in.Status != "active" && in.Status != "disabled" {
		return platform.Invalid("Trạng thái tài khoản không hợp lệ")
	}
	if in.Role != "" && in.Role != "user" && in.Role != "admin" {
		return platform.Invalid("Vai trò không hợp lệ")
	}
	if in.Status == "" && in.Role == "" {
		return platform.Invalid("Không có thay đổi")
	}
	if in.Status == "disabled" && strings.TrimSpace(in.Reason) == "" {
		return platform.Invalid("Cần lý do khóa tài khoản")
	}
	id := r.PathValue("id")
	if id == platform.Current(r).UserID && in.Status == "disabled" {
		return platform.Rule("Không thể tự khóa tài khoản quản trị")
	}
	base, err := requestVersion(r)
	if err != nil {
		return err
	}
	var revision int64
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtext('admin_roles'))"); err != nil {
			return err
		}
		if err := s.lockAdministrator(r.Context(), tx, platform.Current(r).UserID); err != nil {
			return err
		}
		var old json.RawMessage
		var role, status, email string
		if err := tx.QueryRow(r.Context(), "SELECT system_role,status,email,version,to_jsonb(u)-'password_hash' FROM identity.users u WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", id).Scan(&role, &status, &email, &revision, &old); err != nil {
			return err
		}
		if base != revision {
			return platform.Conflict()
		}
		if role == "admin" && (in.Role == "user" || in.Status == "disabled") {
			var count int
			if err := tx.QueryRow(r.Context(), "SELECT count(*) FROM identity.users WHERE system_role='admin' AND status='active' AND deleted_at IS NULL").Scan(&count); err != nil {
				return err
			}
			if count <= 1 {
				return platform.Rule("Không thể hạ quyền quản trị viên cuối cùng")
			}
		}
		if in.Role == "" {
			in.Role = role
		}
		if in.Status == "" {
			in.Status = status
		}
		if err := tx.QueryRow(r.Context(), "UPDATE identity.users SET system_role=$2,status=$3,lock_reason=NULLIF($4,''),token_version=token_version+1,version=version+1 WHERE id=$1 RETURNING version", id, in.Role, in.Status, in.Reason).Scan(&revision); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET revoked_at=$2,revoke_reason='admin' WHERE user_id=$1 AND revoked_at IS NULL", id, s.db.Clock.Now()); err != nil {
			return err
		}
		if err := s.audit(r.Context(), tx, platform.Current(r).UserID, "user.update", id, old, in); err != nil {
			return err
		}
		if in.Status == "disabled" {
			return s.db.Enqueue(r.Context(), tx, "email", "disabled:"+id+":"+platform.ID(), map[string]any{"to": email, "subject": "Tài khoản bị khóa", "body": in.Reason}, s.db.Clock.Now())
		}
		return nil
	})
	if err != nil {
		return err
	}
	versionTag(w, revision)
	platform.Write(w, 200, map[string]any{"id": id, "version": revision})
	return nil
}
