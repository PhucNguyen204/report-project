package accounts

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RegisterProfile(mux *http.ServeMux) {
	for pattern, handler := range map[string]platform.Handler{
		"GET /it4788/v1/me": s.getProfile, "PATCH /it4788/v1/me": s.updateProfile,
		"GET /it4788/v1/me/settings": s.getSettings, "PUT /it4788/v1/me/settings": s.setSettings,
		"PUT /it4788/v1/me/password": s.changePassword, "POST /it4788/v1/auth/password/forgot": s.forgotPassword,
		"POST /it4788/v1/auth/password/reset": s.resetPassword,
	} {
		mux.HandleFunc(pattern, platform.Handle(handler))
	}
}
func (s *Service) getProfile(w http.ResponseWriter, r *http.Request) error {
	var id, email, name, locale string
	var avatar, lastSpace *string
	var revision int64
	err := s.db.Pool.QueryRow(r.Context(), "SELECT id::text,email,full_name,locale,avatar_object_id::text,last_space_id::text,version FROM identity.users WHERE id=$1 AND deleted_at IS NULL", platform.Current(r).UserID).Scan(&id, &email, &name, &locale, &avatar, &lastSpace, &revision)
	if err != nil {
		return err
	}
	versionTag(w, revision)
	platform.Write(w, 200, map[string]any{"id": id, "email": email, "fullName": name, "locale": locale, "avatarObjectId": avatar, "lastSpaceId": lastSpace, "version": revision})
	return nil
}
func (s *Service) updateProfile(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		FullName *string `json:"fullName"`
		Locale   *string `json:"locale"`
		Avatar   *string `json:"avatarObjectId"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.FullName != nil {
		*in.FullName = strings.TrimSpace(*in.FullName)
		n := utf8.RuneCountInString(*in.FullName)
		if n < 2 || n > 50 {
			return platform.Invalid("Họ tên phải có 2–50 ký tự")
		}
	}
	if in.Locale != nil && *in.Locale != "vi" && *in.Locale != "en" {
		return platform.Invalid("Ngôn ngữ không hợp lệ")
	}
	uid := platform.Current(r).UserID
	base, err := requestVersion(r)
	if err != nil {
		return err
	}
	var revision int64
	overwritten := []string{}
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var name, locale string
		var avatar *string
		if err := tx.QueryRow(r.Context(), "SELECT full_name,locale,avatar_object_id::text,version FROM identity.users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", uid).Scan(&name, &locale, &avatar, &revision); err != nil {
			return err
		}
		patch := map[string]any{}
		if in.FullName != nil {
			patch["fullName"] = *in.FullName
		}
		if in.Locale != nil {
			patch["locale"] = *in.Locale
		}
		if in.Avatar != nil {
			var desired any
			if *in.Avatar != "" {
				desired = *in.Avatar
			}
			patch["avatarObjectId"] = desired
		}
		var err error
		overwritten, err = overwrittenFields(r.Context(), tx, "identity.users", uid, base, revision, patch, map[string]any{"fullName": name, "locale": locale, "avatarObjectId": avatar})
		if err != nil {
			return err
		}
		if in.FullName != nil {
			name = *in.FullName
		}
		if in.Locale != nil {
			locale = *in.Locale
		}
		if in.Avatar != nil {
			if *in.Avatar == "" {
				avatar = nil
			} else {
				if s.ValidateMedia == nil {
					return platform.Rule("Dịch vụ xác thực ảnh chưa được cấu hình")
				}
				if err := s.ValidateMedia(r.Context(), tx, uid, "avatar", uid, *in.Avatar); err != nil {
					return err
				}
				avatar = in.Avatar
			}
		}
		if err := tx.QueryRow(r.Context(), "UPDATE identity.users SET full_name=$2,locale=$3,avatar_object_id=$4,version=version+1 WHERE id=$1 RETURNING version", uid, name, locale, avatar).Scan(&revision); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), "SELECT space_id::text FROM household.space_members WHERE user_id=$1 ORDER BY space_id", uid)
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
			if err = s.db.Activity(r.Context(), tx, sid, uid, "member.updated", uid, map[string]any{"fullName": name, "avatarObjectId": avatar}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	versionTag(w, revision)
	platform.Write(w, 200, map[string]any{"updated": true, "version": revision, "overwrittenFields": overwritten})
	return nil
}
func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	var days *int
	var expiry, assignment, activity bool
	var runAt string
	err := s.db.Pool.QueryRow(r.Context(), "SELECT reminder_days,expiry_notify_on,assignment_notify_on,activity_notify_on FROM identity.user_settings WHERE user_id=$1", platform.Current(r).UserID).Scan(&days, &expiry, &assignment, &activity)
	if err != nil {
		return err
	}
	if err = s.db.Pool.QueryRow(r.Context(), "SELECT value #>> '{}' FROM admin.system_configs WHERE key='reminder.runAt'").Scan(&runAt); err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"reminderDays": days, "expiryNotifyOn": expiry, "assignmentNotifyOn": assignment, "activityNotifyOn": activity, "reminderRunAt": runAt, "defaultReminderDays": s.configInt(r.Context(), s.db.Pool, "reminder.defaultDays", 3)})
	return nil
}
func (s *Service) setSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Days       *int  `json:"reminderDays"`
		Expiry     *bool `json:"expiryNotifyOn"`
		Assignment *bool `json:"assignmentNotifyOn"`
		Activity   *bool `json:"activityNotifyOn"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.Days != nil && (*in.Days < 1 || *in.Days > 7) {
		return platform.Invalid("Ngưỡng nhắc từ 1 đến 7 ngày")
	}
	uid := platform.Current(r).UserID
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), "UPDATE identity.user_settings SET reminder_days=$2,expiry_notify_on=COALESCE($3,expiry_notify_on),assignment_notify_on=COALESCE($4,assignment_notify_on),activity_notify_on=COALESCE($5,activity_notify_on) WHERE user_id=$1", uid, in.Days, in.Expiry, in.Assignment, in.Activity)
		if err != nil {
			return err
		}
		return s.db.Enqueue(r.Context(), tx, "settings.changed", platform.ID(), map[string]any{"userId": uid}, s.db.Clock.Now())
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"updated": true})
	return nil
}
func (s *Service) forgotPassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email string `json:"email"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var uid string
		err := tx.QueryRow(r.Context(), "SELECT id::text FROM identity.users WHERE email=$1 AND deleted_at IS NULL AND status='active' FOR UPDATE", strings.ToLower(in.Email)).Scan(&uid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		err = s.sendOTP(r.Context(), tx, uid, strings.ToLower(in.Email), "reset_password")
		var rate *platform.Error
		if errors.As(err, &rate) && rate.Status == 429 {
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"accepted": true})
	return nil
}
func (s *Service) resetPassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Token    string `json:"resetToken"`
		Password string `json:"newPassword"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if err := validatePassword(in.Password); err != nil {
		return err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return err
	}
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var uid, otpID string
		err := tx.QueryRow(r.Context(), "SELECT u.id::text,o.id::text FROM identity.users u JOIN identity.otp_challenges o ON o.user_id=u.id WHERE u.email=$1 AND u.deleted_at IS NULL AND o.purpose='reset_password' AND o.reset_token_hash=$2 AND o.reset_expires_at>$3 FOR UPDATE OF u,o", strings.ToLower(in.Email), secretHash(in.Token), s.db.Clock.Now()).Scan(&uid, &otpID)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.Err(400, 3006, "Mã đặt lại mật khẩu không hợp lệ")
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.users SET password_hash=$2,failed_login_count=0,locked_until=NULL,token_version=token_version+1 WHERE id=$1", uid, hash); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.otp_challenges SET reset_token_hash=NULL,reset_expires_at=NULL WHERE id=$1", otpID); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET revoked_at=$2,revoke_reason='password_reset' WHERE user_id=$1 AND revoked_at IS NULL", uid, s.db.Clock.Now())
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"reset": true})
	return nil
}
func (s *Service) passwordMatches(ctx context.Context, userID, password string) (bool, error) {
	var hash string
	err := s.db.Pool.QueryRow(ctx, "SELECT password_hash FROM identity.users WHERE id=$1 AND deleted_at IS NULL", userID).Scan(&hash)
	if err != nil {
		return false, err
	}
	if verifyPassword(hash, password) {
		return true, nil
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	_, err = tx.Exec(ctx, "UPDATE identity.users SET failed_login_count=LEAST(failed_login_count+1,5),locked_until=CASE WHEN failed_login_count>=4 THEN $2 ELSE locked_until END WHERE id=$1", userID, s.db.Clock.Now().Add(15*60*1e9))
	if err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}
func (s *Service) changePassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Current string `json:"currentPassword"`
		Next    string `json:"newPassword"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if err := validatePassword(in.Next); err != nil {
		return err
	}
	if in.Next == in.Current {
		return platform.Invalid("Mật khẩu mới phải khác mật khẩu cũ")
	}
	p := platform.Current(r)
	ok, err := s.passwordMatches(r.Context(), p.UserID, in.Current)
	if err != nil {
		return err
	}
	if !ok {
		return platform.Invalid("Mật khẩu hiện tại không đúng")
	}
	hash, err := hashPassword(in.Next)
	if err != nil {
		return err
	}
	var result map[string]any
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var currentHash string
		if err := tx.QueryRow(r.Context(), "SELECT password_hash FROM identity.users WHERE id=$1 FOR UPDATE", p.UserID).Scan(&currentHash); err != nil {
			return err
		}
		if !verifyPassword(currentHash, in.Current) {
			return platform.Conflict()
		}
		var device, os string
		if err := tx.QueryRow(r.Context(), "SELECT device_id::text,platform FROM identity.refresh_sessions WHERE user_id=$1 AND family_id=$2 AND revoked_at IS NULL AND rotated_at IS NULL ORDER BY created_at DESC LIMIT 1", p.UserID, p.SessionID).Scan(&device, &os); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE identity.users SET password_hash=$2,token_version=token_version+1,failed_login_count=0,locked_until=NULL WHERE id=$1", p.UserID, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET revoked_at=$2,revoke_reason='password_change' WHERE user_id=$1 AND revoked_at IS NULL", p.UserID, s.db.Clock.Now()); err != nil {
			return err
		}
		var err error
		result, err = s.newSession(r.Context(), tx, p.UserID, device, os, p.SessionID)
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, result)
	return nil
}
