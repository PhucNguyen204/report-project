package media

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"time"
)

type UploadInput struct {
	Purpose     string `json:"purpose"`
	TargetID    string `json:"targetId"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}
type Grant struct {
	ObjectID  string            `json:"objectId"`
	URL       string            `json:"uploadUrl"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}
type Service struct {
	db    *platform.DB
	store *S3Store
}

func New(db *platform.DB, store *S3Store) *Service { return &Service{db, store} }
func (s *Service) actorRole(ctx context.Context, tx pgx.Tx, user string) (string, error) {
	var role string
	err := tx.QueryRow(ctx, "SELECT system_role FROM identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR SHARE", user).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.NotFound()
	}
	return role, err
}
func (s *Service) authorize(ctx context.Context, tx pgx.Tx, user, purpose, target string) error {
	systemRole, err := s.actorRole(ctx, tx, user)
	if err != nil {
		return err
	}
	switch purpose {
	case "avatar":
		if user != target {
			return platform.NotFound()
		}
		return nil
	case "space":
		if systemRole != "user" {
			return platform.NotFound()
		}
		role, err := s.db.LockMember(ctx, tx, target, user)
		if err != nil {
			return err
		}
		if role != "leader" {
			return platform.Forbidden()
		}
		return nil
	case "recipe":
		var sid *string
		var owner, kind string
		err := tx.QueryRow(ctx, "SELECT space_id::text,created_by::text,kind FROM recipe.recipes WHERE id=$1 AND deleted_at IS NULL FOR SHARE", target).Scan(&sid, &owner, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		if kind == "template" {
			if systemRole != "admin" {
				return platform.NotFound()
			}
			return nil
		}
		if systemRole != "user" || sid == nil {
			return platform.NotFound()
		}
		role, err := s.db.LockMember(ctx, tx, *sid, user)
		if err != nil {
			return err
		}
		if user != owner && role != "leader" {
			return platform.Forbidden()
		}
		return nil
	case "food":
		if systemRole != "user" {
			return platform.NotFound()
		}
		var sid string
		err := tx.QueryRow(ctx, "SELECT owner_space_id::text FROM catalog.foods WHERE id=$1 AND scope='space' AND status!='inactive' FOR SHARE", target).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		_, err = s.db.LockMember(ctx, tx, sid, user)
		return err
	default:
		return platform.Invalid("Mục đích ảnh không hợp lệ")
	}
}
func (s *Service) Upload(ctx context.Context, user string, in UploadInput) (Grant, error) {
	out := Grant{}
	if (in.ContentType != "image/png" && in.ContentType != "image/jpeg") || in.Size <= 0 || in.Size > 5*1024*1024 {
		return out, platform.Invalid("Ảnh JPEG hoặc PNG tối đa5MB")
	}
	if in.TargetID == "" {
		return out, platform.Invalid("Thiếu đối tượng ảnh")
	}
	out.ObjectID = platform.ID()
	out.ExpiresAt = s.db.Clock.Now().Add(5 * time.Minute)
	key := "staging/" + user + "/" + out.ObjectID
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if err := s.authorize(ctx, tx, user, in.Purpose, in.TargetID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO media.objects(id,uploaded_by,purpose,target_id,content_type,size,staging_key,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", out.ObjectID, user, in.Purpose, in.TargetID, in.ContentType, in.Size, key, out.ExpiresAt)
		return err
	})
	if err != nil {
		return Grant{}, err
	}
	out.URL, out.Headers, err = s.store.uploadURL(ctx, key, in.ContentType, in.Size)
	return out, err
}

type object struct {
	purpose, target, contentType, stagingKey, status string
	size                                             int64
	expires                                          time.Time
}

func (s *Service) owned(ctx context.Context, tx pgx.Tx, user, id string) (object, error) {
	var o object
	if _, err := s.actorRole(ctx, tx, user); err != nil {
		return o, err
	}
	query := "SELECT purpose,target_id::text,content_type,size,staging_key,status,expires_at FROM media.objects WHERE id=$1 AND uploaded_by=$2"
	read := func(lock string) error {
		err := tx.QueryRow(ctx, query+lock, id, user).Scan(&o.purpose, &o.target, &o.contentType, &o.size, &o.stagingKey, &o.status, &o.expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		return err
	}
	if err := read(""); err != nil {
		return o, err
	}
	if err := s.authorize(ctx, tx, user, o.purpose, o.target); err != nil {
		return o, err
	}
	err := read(" FOR UPDATE")
	return o, err
}
func (s *Service) Complete(ctx context.Context, user, id string) error {
	var o object
	err := s.db.Tx(ctx, func(tx pgx.Tx) error { var err error; o, err = s.owned(ctx, tx, user, id); return err })
	if err != nil {
		return err
	}
	if o.status == "ready" {
		return nil
	}
	if !s.db.Clock.Now().Before(o.expires) {
		return platform.Rule("Phiên tải ảnh đã hết hạn")
	}
	data, contentType, err := s.store.readImage(ctx, o.stagingKey)
	if err != nil {
		return platform.Rule("Chưa nhận được ảnh")
	}
	if int64(len(data)) != o.size || contentType != o.contentType || http.DetectContentType(data) != o.contentType {
		return platform.Invalid("Nội dung ảnh không khớp")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return platform.Invalid("Ảnh không hợp lệ")
	}
	if (format == "png" && o.contentType != "image/png") || (format == "jpeg" && o.contentType != "image/jpeg") {
		return platform.Invalid("Định dạng ảnh không khớp")
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return platform.Invalid("Ảnh không hoàn chỉnh")
	}
	key := "objects/" + id + "/" + platform.ID()
	if err = s.store.Put(ctx, key, o.contentType, data); err != nil {
		return errors.Join(err, s.discard(ctx, key))
	}
	published := false
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		published = false
		current, err := s.owned(ctx, tx, user, id)
		if err != nil {
			return err
		}
		if current.status == "ready" {
			return nil
		}
		if !s.db.Clock.Now().Before(current.expires) {
			return platform.Rule("Phiên tải ảnh đã hết hạn")
		}
		_, err = tx.Exec(ctx, "UPDATE media.objects SET object_key=$2,status='ready',completed_at=$3 WHERE id=$1", id, key, s.db.Clock.Now())
		published = err == nil
		return err
	})
	if err != nil || !published {
		err = errors.Join(err, s.discard(ctx, key))
	}
	return err
}
func (s *Service) Validate(ctx context.Context, tx pgx.Tx, user, purpose, target, id string) error {
	var found bool
	err := tx.QueryRow(ctx, "SELECT true FROM media.objects WHERE id=$1 AND uploaded_by=$2 AND purpose=$3 AND target_id=$4 AND status='ready' FOR SHARE", id, user, purpose, target).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.NotFound()
	}
	if err != nil {
		return err
	}
	return s.authorize(ctx, tx, user, purpose, target)
}
