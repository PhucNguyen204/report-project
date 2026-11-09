package accounts

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
)

type Service struct {
	db            *platform.DB
	key           ed25519.PrivateKey
	pepper        []byte
	dummyHash     string
	ValidateMedia func(context.Context, pgx.Tx, string, string, string, string) error
}

func New(db *platform.DB, key ed25519.PrivateKey) *Service {
	pepper := sha256.Sum256(append([]byte("otp-pepper:"), key...))
	dummy, _ := hashPassword("DummyPassword1")
	return &Service{db: db, key: key, pepper: pepper[:], dummyHash: dummy}
}
func (s *Service) Register(mux *http.ServeMux) {
	for pattern, handler := range map[string]platform.Handler{
		"POST /it4788/v1/auth/register": s.register, "POST /it4788/v1/auth/otp/verify": s.verifyOTP, "POST /it4788/v1/auth/otp/resend": s.resendOTP,
		"POST /it4788/v1/auth/login": s.login, "POST /it4788/v1/auth/refresh": s.refresh, "POST /it4788/v1/auth/logout": s.logout,
		"POST /it4788/v1/me/devices": s.registerDevice, "DELETE /it4788/v1/me/devices/{deviceId}": s.deleteDevice,
	} {
		mux.HandleFunc(pattern, platform.Handle(handler))
	}
}
func (s *Service) Authenticate(ctx context.Context, token string) (platform.Principal, error) {
	c, err := verifyAccess(s.key.Public().(ed25519.PublicKey), token, s.db.Clock.Now())
	if err != nil {
		return platform.Principal{}, err
	}
	var version int64
	var role, status string
	var deleted *time.Time
	var valid bool
	err = s.db.Pool.QueryRow(ctx, `SELECT u.token_version,u.system_role,u.status,u.deleted_at,EXISTS(SELECT 1 FROM identity.refresh_sessions r WHERE r.user_id=u.id AND r.family_id=$2 AND r.revoked_at IS NULL AND r.expires_at>$3 AND r.rotated_at IS NULL) FROM identity.users u WHERE u.id=$1`, c.Subject, c.Session, s.db.Clock.Now()).Scan(&version, &role, &status, &deleted, &valid)
	if err != nil || deleted != nil || !valid || version != c.Version || role != c.Role {
		return platform.Principal{}, platform.Err(401, 3001, "Phiên không còn hiệu lực")
	}
	if status == "disabled" {
		return platform.Principal{}, platform.Err(403, 3005, "Tài khoản bị khóa")
	}
	if status != "active" {
		return platform.Principal{}, platform.Err(401, 3001, "Tài khoản chưa hoạt động")
	}
	return platform.Principal{UserID: c.Subject, SessionID: c.Session, SystemRole: role}, nil
}
func (s *Service) otpHash(email, purpose, code string) []byte {
	m := hmac.New(sha256.New, s.pepper)
	m.Write([]byte(strings.ToLower(email) + ":" + purpose + ":" + code))
	return m.Sum(nil)
}
func secretHash(secret string) []byte { h := sha256.Sum256([]byte(secret)); return h[:] }
func (s *Service) configInt(ctx context.Context, q platform.Querier, key string, fallback int) int {
	var v int
	if err := q.QueryRow(ctx, "SELECT (value #>> '{}')::integer FROM admin.system_configs WHERE key=$1", key).Scan(&v); err != nil {
		return fallback
	}
	return v
}
func (s *Service) sendOTP(ctx context.Context, tx pgx.Tx, userID, email, purpose string) error {
	now := s.db.Clock.Now()
	var latest *time.Time
	var count int
	if err := tx.QueryRow(ctx, "SELECT max(sent_at),count(*) FILTER(WHERE sent_at>$3) FROM identity.otp_challenges WHERE user_id=$1 AND purpose=$2", userID, purpose, now.Add(-time.Hour)).Scan(&latest, &count); err != nil {
		return err
	}
	if latest != nil && now.Sub(*latest) < time.Minute || count >= 5 {
		return platform.Err(429, 4290, "Vui lòng chờ trước khi gửi lại OTP")
	}
	code, err := randomOTP()
	if err != nil {
		return err
	}
	id := platform.ID()
	if _, err = tx.Exec(ctx, "UPDATE identity.otp_challenges SET used_at=$3 WHERE user_id=$1 AND purpose=$2 AND used_at IS NULL", userID, purpose, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO identity.otp_challenges(id,user_id,purpose,code_hash,expires_at,sent_at) VALUES($1,$2,$3,$4,$5,$6)", id, userID, purpose, s.otpHash(email, purpose, code), now.Add(time.Duration(s.configInt(ctx, tx, "otp.ttlMinutes", 5))*time.Minute), now); err != nil {
		return err
	}
	return s.db.Enqueue(ctx, tx, "email", id, map[string]any{"to": email, "purpose": purpose, "code": code}, now)
}
func (s *Service) newSession(ctx context.Context, tx pgx.Tx, userID, device, os, family string) (map[string]any, error) {
	if device == "" {
		device = platform.ID()
	}
	if os == "" {
		os = "android"
	}
	if os != "android" && os != "ios" {
		return nil, platform.Invalid("Nền tảng không hợp lệ")
	}
	if family == "" {
		family = platform.ID()
	}
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	id := platform.ID()
	now := s.db.Clock.Now()
	var role string
	var version int64
	if err = tx.QueryRow(ctx, "SELECT system_role,token_version FROM identity.users WHERE id=$1", userID).Scan(&role, &version); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO identity.refresh_sessions(id,user_id,family_id,token_hash,device_id,platform,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)", id, userID, family, secretHash(secret), device, os, now.Add(30*24*time.Hour)); err != nil {
		return nil, err
	}
	access, err := signAccess(s.key, claims{Subject: userID, Session: family, Role: role, Version: version, Issued: now.Unix(), Expires: now.Add(30 * time.Minute).Unix(), ID: platform.ID()})
	if err != nil {
		return nil, err
	}
	return map[string]any{"accessToken": access, "refreshToken": secret, "expiresIn": 1800, "deviceId": device, "sessionId": id, "userId": userID}, nil
}
func (s *Service) register(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		FullName    string `json:"fullName"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		AcceptTerms bool   `json:"acceptTerms"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	in.Email = strings.ToLower(in.Email)
	if err := validateRegistration(in.FullName, in.Email, in.Password); err != nil {
		return err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return err
	}
	id := platform.ID()
	created := true
	err = s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(r.Context(), "SELECT id::text,status FROM identity.users WHERE email=$1 FOR UPDATE", in.Email).Scan(&id, &status)
		if err == nil {
			if status != "pending" {
				return platform.Err(409, 4003, "Email đã được đăng ký")
			}
			created = false
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		} else {
			if _, err = tx.Exec(r.Context(), "INSERT INTO identity.users(id,email,full_name,password_hash) VALUES($1,$2,$3,$4)", id, in.Email, strings.TrimSpace(in.FullName), hash); err != nil {
				return err
			}
		}
		return s.sendOTP(r.Context(), tx, id, in.Email, "register")
	})
	if err != nil {
		return err
	}
	status := 201
	if !created {
		status = 200
	}
	platform.Write(w, status, map[string]any{"userId": id, "status": "pending"})
	return nil
}
func (s *Service) resendOTP(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.Purpose == "" {
		in.Purpose = "register"
	}
	if in.Purpose != "register" && in.Purpose != "reset_password" {
		return platform.Invalid("Mục đích OTP không hợp lệ")
	}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var id, status string
		err := tx.QueryRow(r.Context(), "SELECT id::text,status FROM identity.users WHERE email=$1 AND deleted_at IS NULL FOR UPDATE", in.Email).Scan(&id, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if in.Purpose == "register" && status != "pending" {
			return platform.Invalid("Tài khoản đã xác thực")
		}
		return s.sendOTP(r.Context(), tx, id, strings.ToLower(in.Email), in.Purpose)
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"accepted": true})
	return nil
}
func (s *Service) verifyOTP(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Purpose  string `json:"purpose"`
		Code     string `json:"code"`
		DeviceID string `json:"deviceId"`
		Platform string `json:"platform"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.Purpose == "" {
		in.Purpose = "register"
	}
	if in.Purpose != "register" && in.Purpose != "reset_password" {
		return platform.Invalid("Mục đích OTP không hợp lệ")
	}
	var result map[string]any
	var outcome error
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		outcome = nil
		var user, status string
		err := tx.QueryRow(r.Context(), "SELECT id::text,status FROM identity.users WHERE email=$1 AND deleted_at IS NULL FOR UPDATE", in.Email).Scan(&user, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			outcome = platform.Err(400, 3006, "OTP sai hoặc đã hết hạn")
			return nil
		}
		if err != nil {
			return err
		}
		var id string
		var hash []byte
		var expires time.Time
		var used *time.Time
		var attempts int
		err = tx.QueryRow(r.Context(), "SELECT id::text,code_hash,expires_at,used_at,attempts FROM identity.otp_challenges WHERE user_id=$1 AND purpose=$2 ORDER BY sent_at DESC LIMIT 1 FOR UPDATE", user, in.Purpose).Scan(&id, &hash, &expires, &used, &attempts)
		if errors.Is(err, pgx.ErrNoRows) || used != nil || attempts >= 5 || !s.db.Clock.Now().Before(expires) {
			outcome = platform.Err(400, 3006, "OTP sai hoặc đã hết hạn")
			return nil
		}
		if err != nil {
			return err
		}
		if !hmac.Equal(hash, s.otpHash(in.Email, in.Purpose, in.Code)) {
			_, err = tx.Exec(r.Context(), "UPDATE identity.otp_challenges SET attempts=attempts+1 WHERE id=$1", id)
			outcome = platform.Err(400, 3006, "OTP sai hoặc đã hết hạn")
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.otp_challenges SET used_at=$2 WHERE id=$1", id, s.db.Clock.Now()); err != nil {
			return err
		}
		if in.Purpose == "reset_password" {
			token, err := randomSecret()
			if err != nil {
				return err
			}
			_, err = tx.Exec(r.Context(), "UPDATE identity.otp_challenges SET reset_token_hash=$2,reset_expires_at=$3 WHERE id=$1", id, secretHash(token), s.db.Clock.Now().Add(10*time.Minute))
			result = map[string]any{"resetToken": token}
			return err
		}
		if status != "pending" {
			outcome = platform.Err(400, 3006, "OTP không còn hiệu lực")
			return nil
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.users SET status='active' WHERE id=$1", user); err != nil {
			return err
		}
		sid := platform.ID()
		if _, err = tx.Exec(r.Context(), "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Không gian cá nhân',$2)", sid, user); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','personal')", sid, user); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO identity.user_settings(user_id) VALUES($1)", user); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.users SET last_space_id=$2 WHERE id=$1", user, sid); err != nil {
			return err
		}
		result, err = s.newSession(r.Context(), tx, user, in.DeviceID, in.Platform, "")
		if err == nil {
			result["spaceId"] = sid
		}
		return err
	})
	if err != nil {
		return err
	}
	if outcome != nil {
		return outcome
	}
	platform.Write(w, 200, result)
	return nil
}
func (s *Service) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		DeviceID string `json:"deviceId"`
		Platform string `json:"platform"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	var result map[string]any
	var outcome error
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		outcome = nil
		var id, hash, status string
		var count int
		var locked, deleted *time.Time
		err := tx.QueryRow(r.Context(), "SELECT id::text,password_hash,status,failed_login_count,locked_until,deleted_at FROM identity.users WHERE email=$1 FOR UPDATE", in.Email).Scan(&id, &hash, &status, &count, &locked, &deleted)
		if errors.Is(err, pgx.ErrNoRows) || deleted != nil {
			verifyPassword(s.dummyHash, in.Password)
			outcome = platform.Err(401, 3003, "Email hoặc mật khẩu không đúng")
			return nil
		}
		if err != nil {
			return err
		}
		now := s.db.Clock.Now()
		if locked != nil && now.Before(*locked) {
			outcome = platform.Err(423, 3004, "Tài khoản tạm khóa 15 phút")
			return nil
		}
		if locked != nil {
			count = 0
		}
		if !verifyPassword(hash, in.Password) {
			count++
			var until *time.Time
			if count >= 5 {
				t := now.Add(15 * time.Minute)
				until = &t
			}
			_, err = tx.Exec(r.Context(), "UPDATE identity.users SET failed_login_count=$2,locked_until=$3 WHERE id=$1", id, count, until)
			outcome = platform.Err(401, 3003, "Email hoặc mật khẩu không đúng")
			if count == 5 {
				outcome = platform.Err(423, 3004, "Tài khoản tạm khóa 15 phút")
			}
			return err
		}
		if status == "disabled" {
			outcome = platform.Err(403, 3005, "Tài khoản bị khóa")
			return nil
		}
		if status != "active" {
			err = s.sendOTP(r.Context(), tx, id, strings.ToLower(in.Email), "register")
			var typed *platform.Error
			if err != nil && (!errors.As(err, &typed) || typed.Status != 429) {
				return err
			}
			outcome = platform.Err(403, 3009, "Tài khoản chưa xác thực")
			return nil
		}
		if _, err = tx.Exec(r.Context(), "UPDATE identity.users SET failed_login_count=0,locked_until=NULL,last_login_at=$2 WHERE id=$1", id, now); err != nil {
			return err
		}
		result, err = s.newSession(r.Context(), tx, id, in.DeviceID, in.Platform, "")
		return err
	})
	if err != nil {
		return err
	}
	if outcome != nil {
		return outcome
	}
	platform.Write(w, 200, result)
	return nil
}
func (s *Service) refresh(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		RefreshToken string `json:"refreshToken"`
		DeviceID     string `json:"deviceId"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	var result map[string]any
	var outcome error
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		outcome = nil
		var id, user, family, device, os string
		var expires time.Time
		var rotated, revoked *time.Time
		err := tx.QueryRow(r.Context(), "SELECT id::text,user_id::text,family_id::text,device_id::text,platform,expires_at,rotated_at,revoked_at FROM identity.refresh_sessions WHERE token_hash=$1 FOR UPDATE", secretHash(in.RefreshToken)).Scan(&id, &user, &family, &device, &os, &expires, &rotated, &revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			outcome = platform.Err(401, 3001, "Phiên không hợp lệ")
			return nil
		}
		if err != nil {
			return err
		}
		now := s.db.Clock.Now()
		if revoked != nil || !now.Before(expires) {
			outcome = platform.Err(401, 3001, "Phiên đã hết hiệu lực")
			return nil
		}
		if rotated != nil {
			if device != in.DeviceID || now.Sub(*rotated) > 30*time.Second {
				if _, err = tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET revoked_at=$2,revoke_reason='reuse' WHERE family_id=$1", family, now); err != nil {
					return err
				}
				_, err = tx.Exec(r.Context(), "UPDATE identity.users SET token_version=token_version+1 WHERE id=$1", user)
				outcome = platform.Err(401, 3008, "Mã làm mới bị dùng lại")
				return err
			}
			var successorRotated *time.Time
			var successor string
			err = tx.QueryRow(r.Context(), "SELECT id::text,rotated_at FROM identity.refresh_sessions WHERE id=(SELECT successor_id FROM identity.refresh_sessions WHERE id=$1) FOR UPDATE", id).Scan(&successor, &successorRotated)
			if err != nil {
				return err
			}
			if successorRotated != nil {
				outcome = platform.Err(401, 3008, "Phiên kế nhiệm đã được dùng")
				return nil
			}
			if _, err = tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET revoked_at=$2,revoke_reason='superseded' WHERE id=$1", successor, now); err != nil {
				return err
			}
		} else if device != in.DeviceID {
			outcome = platform.Err(401, 3001, "Thiết bị không hợp lệ")
			return nil
		}
		var status string
		var deleted *time.Time
		if err = tx.QueryRow(r.Context(), "SELECT status,deleted_at FROM identity.users WHERE id=$1 FOR UPDATE", user).Scan(&status, &deleted); err != nil {
			return err
		}
		if status != "active" || deleted != nil {
			outcome = platform.Err(401, 3001, "Tài khoản không hoạt động")
			return nil
		}
		result, err = s.newSession(r.Context(), tx, user, device, os, family)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET rotated_at=COALESCE(rotated_at,$2),successor_id=$3 WHERE id=$1", id, now, result["sessionId"])
		return err
	})
	if err != nil {
		return err
	}
	if outcome != nil {
		return outcome
	}
	platform.Write(w, 200, result)
	return nil
}
func (s *Service) logout(w http.ResponseWriter, r *http.Request) error {
	p := platform.Current(r)
	if p.UserID == "" {
		var err error
		p, err = s.Authenticate(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			return err
		}
	}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), "UPDATE identity.refresh_sessions SET revoked_at=$3,revoke_reason='logout' WHERE user_id=$1 AND family_id=$2", p.UserID, p.SessionID, s.db.Clock.Now())
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"loggedOut": true})
	return nil
}
func (s *Service) registerDevice(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		DeviceID string `json:"deviceId"`
		Token    string `json:"fcmToken"`
		Platform string `json:"platform"`
	}
	if err := platform.Decode(r, &in); err != nil {
		return err
	}
	if in.DeviceID == "" {
		in.DeviceID = platform.ID()
	}
	if len(in.Token) < 1 || len(in.Token) > 4096 || (in.Platform != "android" && in.Platform != "ios") {
		return platform.Invalid("Thiết bị không hợp lệ")
	}
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		var owner string
		err := tx.QueryRow(r.Context(), "SELECT user_id::text FROM identity.devices WHERE id=$1 FOR UPDATE", in.DeviceID).Scan(&owner)
		if err == nil && owner != platform.Current(r).UserID {
			return platform.NotFound()
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(r.Context(), "INSERT INTO identity.devices(id,user_id,fcm_token,platform) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET fcm_token=EXCLUDED.fcm_token,platform=EXCLUDED.platform,updated_at=now()", in.DeviceID, platform.Current(r).UserID, in.Token, in.Platform)
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"deviceId": in.DeviceID})
	return nil
}
func (s *Service) deleteDevice(w http.ResponseWriter, r *http.Request) error {
	err := s.db.Tx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), "DELETE FROM identity.devices WHERE id=$1 AND user_id=$2", r.PathValue("deviceId"), platform.Current(r).UserID)
		if err == nil && tag.RowsAffected() == 0 {
			return platform.NotFound()
		}
		return err
	})
	if err != nil {
		return err
	}
	platform.Write(w, 200, map[string]any{"deleted": true})
	return nil
}
