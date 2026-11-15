package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RegisterGroups(mux *http.ServeMux) {
	for pattern, handler := range map[string]platform.Handler{
		"GET /it4788/v1/spaces": s.listSpaces, "POST /it4788/v1/spaces": s.createSpace,
		"GET /it4788/v1/spaces/{sid}": s.getSpace, "PATCH /it4788/v1/spaces/{sid}": s.updateSpace,
		"GET /it4788/v1/spaces/{sid}/invites/current": s.currentInvite,
		"POST /it4788/v1/spaces/{sid}/invites":        s.createInvite, "DELETE /it4788/v1/spaces/{sid}/invites/current": s.revokeInvite,
		"POST /it4788/v1/spaces/join": s.joinSpace, "POST /it4788/v1/spaces/{sid}/leave": s.leaveSpace,
		"DELETE /it4788/v1/spaces/{sid}/members/{userId}": s.removeMember, "POST /it4788/v1/spaces/{sid}/leader-transfer": s.transferLeader,
	} {
		mux.HandleFunc(pattern, platform.Handle(handler))
	}
}
func groupUser(r *http.Request) error {
	if platform.Current(r).SystemRole == "admin" {
		return platform.NotFound()
	}
	return nil
}
func groupName(name string) error {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	if n < 3 || n > 50 {
		return platform.Invalid("Tên nhóm phải có 3–50 ký tự")
	}
	return nil
}
func (s *Service) listSpaces(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	role, err := s.db.CheckActor(r.Context(), s.db.Pool, platform.Current(r).UserID)
	if err != nil {
		return err
	}
	if role != "user" {
		return platform.NotFound()
	}
	items, err := queryMaps(r.Context(), s.db.Pool, `SELECT s.id::text,s.kind,s.name,m.role,s.change_seq AS "changeSeq",s.version,s.avatar_object_id::text AS "avatarObjectId" FROM household.spaces s JOIN household.space_members m ON m.space_id=s.id WHERE m.user_id=$1 AND s.dissolved_at IS NULL ORDER BY s.kind,s.id`, platform.Current(r).UserID)
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"items": items})
	return nil
}
func (s *Service) createSpace(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
		Copy struct {
			Lists   bool `json:"lists"`
			Lots    bool `json:"lots"`
			Recipes bool `json:"recipes"`
		} `json:"copyPersonalData"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if err := groupName(in.Name); err != nil {
		return err
	}
	uid := platform.Current(r).UserID
	sid := platform.ID()
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var user string
		if err := tx.QueryRow(r.Context(), "SELECT id::text FROM identity.users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", uid).Scan(&user); err != nil {
			return err
		}
		role, err := s.db.LockActor(r.Context(), tx, uid)
		if err != nil {
			return err
		}
		if role != "user" {
			return platform.NotFound()
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM household.space_members WHERE user_id=$1 AND space_kind='family')", uid).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return platform.Rule("Bạn đã tham gia một nhóm gia đình")
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family',$2,$3)", sid, strings.TrimSpace(in.Name), uid); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family')", sid, uid); err != nil {
			return err
		}
		if in.Copy.Lists || in.Copy.Lots || in.Copy.Recipes {
			var source string
			if err := tx.QueryRow(r.Context(), "SELECT space_id::text FROM household.space_members WHERE user_id=$1 AND space_kind='personal'", uid).Scan(&source); err != nil {
				return err
			}
			if err := s.db.Enqueue(r.Context(), tx, "space.copy", sid, map[string]any{"sourceSpaceId": source, "targetSpaceId": sid, "userId": uid, "lists": in.Copy.Lists, "lots": in.Copy.Lots, "recipes": in.Copy.Recipes}, s.db.Clock.Now()); err != nil {
				return err
			}
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "space.created", sid, map[string]any{"name": in.Name})
	})
	if err != nil {
		return err
	}
	platform.Write(w, 201, map[string]any{"id": sid, "kind": "family", "name": strings.TrimSpace(in.Name)})
	return nil
}
func (s *Service) getSpace(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	sid := r.PathValue("sid")
	if _, err := s.db.RequireMember(r.Context(), s.db.Pool, sid, platform.Current(r).UserID); err != nil {
		return err
	}
	var name, kind string
	var seq, revision int64
	if err := s.db.Pool.QueryRow(r.Context(), "SELECT name,kind,change_seq,version FROM household.spaces WHERE id=$1 AND dissolved_at IS NULL", sid).Scan(&name, &kind, &seq, &revision); err != nil {
		return err
	}
	members, err := queryMaps(r.Context(), s.db.Pool, `SELECT m.user_id::text AS "userId",m.role,u.full_name AS "fullName",u.avatar_object_id::text AS "avatarObjectId" FROM household.space_members m JOIN identity.users u ON u.id=m.user_id WHERE m.space_id=$1 ORDER BY m.role='leader' DESC,m.joined_at,m.user_id`, sid)
	if err != nil {
		return err
	}
	recent, err := queryMaps(r.Context(), s.db.Pool, `SELECT a.seq,a.kind,a.entity_id AS "entityId",a.actor_id::text AS "actorId",u.full_name AS "actorName",a.payload,a.created_at AS "createdAt" FROM platform.activities a LEFT JOIN identity.users u ON u.id=a.actor_id WHERE a.space_id=$1 ORDER BY a.seq DESC LIMIT 20`, sid)
	if err != nil {
		return err
	}
	versionTag(w, revision)
	platform.Write(w, 200, map[string]any{"version": revision, "id": sid, "name": name, "kind": kind, "changeSeq": seq, "members": members, "recentActivity": recent})
	return nil
}
func (s *Service) lockFamily(ctx context.Context, tx pgx.Tx, sid, uid string) (string, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", sid); err != nil {
		return "", err
	}
	role, err := s.db.LockMember(ctx, tx, sid, uid)
	if err != nil {
		return "", err
	}
	var kind string
	if err = tx.QueryRow(ctx, "SELECT kind FROM household.spaces WHERE id=$1 AND dissolved_at IS NULL", sid).Scan(&kind); err != nil {
		return "", err
	}
	if kind != "family" {
		return "", platform.Rule("Thao tác không áp dụng cho không gian cá nhân")
	}
	return role, nil
}
func (s *Service) updateSpace(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	var in struct {
		Name   *string `json:"name"`
		Avatar *string `json:"avatarObjectId"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.Name != nil {
		if err := groupName(*in.Name); err != nil {
			return err
		}
	}
	sid, uid := r.PathValue("sid"), platform.Current(r).UserID
	base, err := requestVersion(r)
	if err != nil {
		return err
	}
	var revision int64
	overwritten := []string{}
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		role, err := s.lockFamily(r.Context(), tx, sid, uid)
		if err != nil {
			return err
		}
		if role != "leader" {
			return platform.Forbidden()
		}
		var name string
		var avatar *string
		if err = tx.QueryRow(r.Context(), "SELECT name,avatar_object_id::text,version FROM household.spaces WHERE id=$1", sid).Scan(&name, &avatar, &revision); err != nil {
			return err
		}
		patch := map[string]any{}
		if in.Name != nil {
			patch["name"] = strings.TrimSpace(*in.Name)
		}
		if in.Avatar != nil {
			var desired any
			if *in.Avatar != "" {
				desired = *in.Avatar
			}
			patch["avatarObjectId"] = desired
		}
		overwritten, err = overwrittenFields(r.Context(), tx, "household.spaces", sid, base, revision, patch, map[string]any{"name": name, "avatarObjectId": avatar})
		if err != nil {
			return err
		}
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.Avatar != nil {
			if *in.Avatar == "" {
				avatar = nil
			} else {
				if s.ValidateMedia == nil {
					return platform.Rule("Dịch vụ xác thực ảnh chưa được cấu hình")
				}
				if err = s.ValidateMedia(r.Context(), tx, uid, "space", sid, *in.Avatar); err != nil {
					return err
				}
				avatar = in.Avatar
			}
		}
		if err = tx.QueryRow(r.Context(), "UPDATE household.spaces SET name=$2,avatar_object_id=$3,version=version+1 WHERE id=$1 RETURNING version", sid, name, avatar).Scan(&revision); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "space.updated", sid, map[string]any{"name": name})
	})
	if err != nil {
		return err
	}
	versionTag(w, revision)
	platform.Write(w, 200, map[string]any{"id": sid, "version": revision, "overwrittenFields": overwritten})
	return nil
}
func randomInvite() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}
func (s *Service) inviteCapacity(ctx context.Context, tx pgx.Tx, sid string) (int, int, error) {
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM household.space_members WHERE space_id=$1", sid).Scan(&count); err != nil {
		return 0, 0, err
	}
	maximum := s.configInt(ctx, tx, "group.maxMembers", 10)
	if count >= maximum {
		return count, maximum, platform.Rule("Nhóm đã đủ thành viên")
	}
	return count, maximum, nil
}
func (s *Service) currentInvite(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	sid, uid := r.PathValue("sid"), platform.Current(r).UserID
	out := map[string]any{"status": "none", "code": nil, "expiresAt": nil, "link": nil}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		role, err := s.lockFamily(r.Context(), tx, sid, uid)
		if err != nil {
			return err
		}
		if role != "leader" {
			return platform.Forbidden()
		}
		count, maximum, err := s.inviteCapacity(r.Context(), tx, sid)
		if err != nil {
			return err
		}
		out["memberCount"], out["maxMembers"] = count, maximum
		var code string
		var expires time.Time
		err = tx.QueryRow(r.Context(), "SELECT code,expires_at FROM household.invite_codes WHERE space_id=$1 AND revoked_at IS NULL", sid).Scan(&code, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out["status"], out["code"], out["expiresAt"], out["link"] = "active", code, expires, "https://dicho.example/join/"+code
		if !s.db.Clock.Now().Before(expires) {
			out["status"] = "expired"
		}
		return nil
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, out)
	return nil
}
func (s *Service) createInvite(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	sid, uid := r.PathValue("sid"), platform.Current(r).UserID
	code, err := randomInvite()
	if err != nil {
		return err
	}
	expires := s.db.Clock.Now()
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		role, err := s.lockFamily(r.Context(), tx, sid, uid)
		if err != nil {
			return err
		}
		if role != "leader" {
			return platform.Forbidden()
		}
		if _, _, err = s.inviteCapacity(r.Context(), tx, sid); err != nil {
			return err
		}
		expires = s.db.Clock.Now().Add(time.Duration(s.configInt(r.Context(), tx, "invite.ttlHours", 48)) * time.Hour)
		if _, err = tx.Exec(r.Context(), "UPDATE household.invite_codes SET revoked_at=$2 WHERE space_id=$1 AND revoked_at IS NULL", sid, s.db.Clock.Now()); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), "INSERT INTO household.invite_codes(id,space_id,code,created_by,expires_at) VALUES($1,$2,$3,$4,$5)", platform.ID(), sid, code, uid, expires)
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 201, map[string]any{"code": code, "expiresAt": expires, "link": "https://dicho.example/join/" + code})
	return nil
}
func (s *Service) revokeInvite(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	sid, uid := r.PathValue("sid"), platform.Current(r).UserID
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		role, err := s.lockFamily(r.Context(), tx, sid, uid)
		if err != nil {
			return err
		}
		if role != "leader" {
			return platform.Forbidden()
		}
		_, err = tx.Exec(r.Context(), "UPDATE household.invite_codes SET revoked_at=$2 WHERE space_id=$1 AND revoked_at IS NULL", sid, s.db.Clock.Now())
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"revoked": true})
	return nil
}
func (s *Service) joinAttempt(ctx context.Context, user string) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '3s'; SET LOCAL statement_timeout = '10s'"); err != nil {
		return err
	}
	role, err := s.db.LockActor(ctx, tx, user)
	if err != nil {
		return err
	}
	if role != "user" {
		return platform.NotFound()
	}
	var n int
	err = tx.QueryRow(ctx, `INSERT INTO household.join_attempts(user_id,window_start,hits) VALUES($1,$2,1) ON CONFLICT(user_id) DO UPDATE SET hits=CASE WHEN household.join_attempts.window_start<=$2-interval '1 hour' THEN 1 ELSE household.join_attempts.hits+1 END,window_start=CASE WHEN household.join_attempts.window_start<=$2-interval '1 hour' THEN $2 ELSE household.join_attempts.window_start END RETURNING hits`, user, s.db.Clock.Now()).Scan(&n)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if n > 10 {
		return platform.Err(429, 4290, "Chỉ được nhập mã mời tối đa 10 lần mỗi giờ")
	}
	return nil
}
func normalizeInvite(code string) (string, error) {
	code = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, code)
	if len(code) != 8 {
		return "", platform.Invalid("Mã mời phải có 8 ký tự hợp lệ")
	}
	for _, c := range code {
		if !strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", c) {
			return "", platform.Invalid("Mã mời phải có 8 ký tự hợp lệ")
		}
	}
	return code, nil
}
func (s *Service) joinSpace(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	var in struct {
		Code   string `json:"code"`
		DryRun bool   `json:"dryRun"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	uid := platform.Current(r).UserID
	if err := s.joinAttempt(r.Context(), uid); err != nil {
		return err
	}
	var err error
	in.Code, err = normalizeInvite(in.Code)
	if err != nil {
		return err
	}
	var result map[string]any
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var sid string
		err := tx.QueryRow(r.Context(), "SELECT space_id::text FROM household.invite_codes WHERE code=$1", in.Code).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Rule("Mã mời sai hoặc đã hết hạn")
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtext($1))", sid); err != nil {
			return err
		}
		var name string
		var count int
		err = tx.QueryRow(r.Context(), `SELECT s.name,(SELECT count(*) FROM household.space_members WHERE space_id=s.id) FROM household.invite_codes i JOIN household.spaces s ON s.id=i.space_id WHERE i.code=$1 AND i.revoked_at IS NULL AND i.expires_at>$2 AND s.dissolved_at IS NULL`, in.Code, s.db.Clock.Now()).Scan(&name, &count)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Rule("Mã mời sai hoặc đã hết hạn")
		}
		if err != nil {
			return err
		}
		var user string
		if err = tx.QueryRow(r.Context(), "SELECT id::text FROM identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE", uid).Scan(&user); err != nil {
			return err
		}
		role, err := s.db.LockActor(r.Context(), tx, uid)
		if err != nil {
			return err
		}
		if role != "user" {
			return platform.NotFound()
		}
		var currentSpace, memberRole string
		var joined time.Time
		err = tx.QueryRow(r.Context(), "SELECT space_id::text,role,joined_at FROM household.space_members WHERE user_id=$1 AND space_kind='family'", uid).Scan(&currentSpace, &memberRole, &joined)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if currentSpace != "" && currentSpace != sid {
			return platform.Rule("Bạn đã tham gia một nhóm gia đình")
		}
		maximum := s.configInt(r.Context(), tx, "group.maxMembers", 10)
		if currentSpace == "" && count >= maximum {
			return platform.Rule("Nhóm đã đủ thành viên")
		}
		var leader, leaderName string
		if err = tx.QueryRow(r.Context(), "SELECT m.user_id::text,u.full_name FROM household.space_members m JOIN identity.users u ON u.id=m.user_id WHERE m.space_id=$1 AND m.role='leader'", sid).Scan(&leader, &leaderName); err != nil {
			return err
		}
		result = map[string]any{"id": sid, "name": name, "leaderName": leaderName, "memberCount": count, "maxMembers": maximum, "dryRun": in.DryRun, "role": "", "joinedAt": nil}
		if currentSpace == sid {
			result["role"], result["joinedAt"] = memberRole, joined
			return nil
		}
		if in.DryRun {
			return nil
		}
		if err = tx.QueryRow(r.Context(), "INSERT INTO household.space_members(space_id,user_id,role,space_kind,joined_at) VALUES($1,$2,'member','family',$3) RETURNING joined_at", sid, uid, s.db.Clock.Now()).Scan(&joined); err != nil {
			return err
		}
		result["role"], result["joinedAt"], result["memberCount"] = "member", joined, count+1
		if err = s.db.Enqueue(r.Context(), tx, "notification", "join:"+sid+":"+uid, map[string]any{"userId": leader, "spaceId": sid, "title": "Thành viên mới", "body": "Một thành viên đã tham gia nhóm " + name, "type": "activity"}, s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "member.joined", uid, map[string]any{"userId": uid})
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, result)
	return nil
}
func (s *Service) leaveSpace(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	sid, uid := r.PathValue("sid"), platform.Current(r).UserID
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		role, err := s.lockFamily(r.Context(), tx, sid, uid)
		if err != nil {
			return err
		}
		var n int
		if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM household.space_members WHERE space_id=$1", sid).Scan(&n); err != nil {
			return err
		}
		if role == "leader" && n > 1 {
			return platform.Rule("Hãy chuyển quyền trưởng nhóm trước khi rời nhóm")
		}
		if _, err = tx.Exec(r.Context(), "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", sid, uid); err != nil {
			return err
		}
		if s.db.MemberLeave != nil {
			if err = s.db.MemberLeave(r.Context(), tx, sid, uid); err != nil {
				return err
			}
		}
		if role == "leader" {
			if _, err = tx.Exec(r.Context(), "UPDATE household.invite_codes SET revoked_at=$2 WHERE space_id=$1 AND revoked_at IS NULL", sid, s.db.Clock.Now()); err != nil {
				return err
			}
			if _, err = tx.Exec(r.Context(), "UPDATE household.spaces SET dissolved_at=$2 WHERE id=$1", sid, s.db.Clock.Now()); err != nil {
				return err
			}
			if err = s.db.Enqueue(r.Context(), tx, "space.purge", sid, map[string]any{"spaceId": sid}, s.db.Clock.Now()); err != nil {
				return err
			}
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "member.left", uid, map[string]any{"dissolved": role == "leader"})
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"left": true})
	return nil
}
func (s *Service) removeMember(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	sid, uid, target := r.PathValue("sid"), platform.Current(r).UserID, r.PathValue("userId")
	if uid == target {
		return platform.Rule("Dùng chức năng rời nhóm để rời nhóm")
	}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		role, err := s.lockFamily(r.Context(), tx, sid, uid)
		if err != nil {
			return err
		}
		if role != "leader" {
			return platform.Forbidden()
		}
		tag, err := tx.Exec(r.Context(), "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2 AND role='member'", sid, target)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return platform.NotFound()
		}
		if s.db.MemberLeave != nil {
			if err = s.db.MemberLeave(r.Context(), tx, sid, target); err != nil {
				return err
			}
		}
		if err = s.db.Enqueue(r.Context(), tx, "notification", "remove:"+sid+":"+target+":"+platform.ID(), map[string]any{"userId": target, "title": "Bạn đã rời nhóm", "body": "Trưởng nhóm đã xóa bạn khỏi nhóm", "type": "activity"}, s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "member.removed", target, map[string]any{"userId": target})
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"removed": true})
	return nil
}
func (s *Service) transferLeader(w http.ResponseWriter, r *http.Request) error {
	if err := groupUser(r); err != nil {
		return err
	}
	var in struct {
		UserID string `json:"userId"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	sid, uid := r.PathValue("sid"), platform.Current(r).UserID
	if in.UserID == uid || in.UserID == "" {
		return platform.Invalid("Cần chọn thành viên khác")
	}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtext($1))", sid); err != nil {
			return err
		}
		users := []string{uid, in.UserID}
		sort.Strings(users)
		roles := map[string]string{}
		for _, u := range users {
			role, err := s.db.LockMember(r.Context(), tx, sid, u)
			if err != nil {
				return err
			}
			roles[u] = role
		}
		if roles[uid] != "leader" {
			return platform.Forbidden()
		}
		if roles[in.UserID] != "member" {
			return platform.Rule("Người nhận phải là thành viên trong nhóm")
		}
		var kind string
		if err := tx.QueryRow(r.Context(), "SELECT kind FROM household.spaces WHERE id=$1 AND dissolved_at IS NULL", sid).Scan(&kind); err != nil {
			return err
		}
		if kind != "family" {
			return platform.Rule("Không thể chuyển quyền không gian cá nhân")
		}
		if _, err := tx.Exec(r.Context(), "UPDATE household.space_members SET role='member' WHERE space_id=$1 AND user_id=$2", sid, uid); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE household.space_members SET role='leader' WHERE space_id=$1 AND user_id=$2", sid, in.UserID); err != nil {
			return err
		}
		if err := s.db.Enqueue(r.Context(), tx, "notification", fmt.Sprintf("leader:%s:%s", sid, platform.ID()), map[string]any{"userId": in.UserID, "spaceId": sid, "title": "Quyền trưởng nhóm", "body": "Bạn đã trở thành trưởng nhóm", "type": "activity"}, s.db.Clock.Now()); err != nil {
			return err
		}
		return s.db.Activity(r.Context(), tx, sid, uid, "leader.transferred", in.UserID, map[string]any{"userId": in.UserID})
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"leaderId": in.UserID})
	return nil
}
