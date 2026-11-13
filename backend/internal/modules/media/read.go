package media

import (
	"context"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type Download struct {
	URL       string    `json:"downloadUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Service) canRead(ctx context.Context, tx pgx.Tx, user, purpose, target, id string) error {
	switch purpose {
	case "avatar":
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.users u WHERE u.id=$2 AND u.deleted_at IS NULL AND u.status='active' AND ($1=$2 OR EXISTS(SELECT 1 FROM household.space_members a JOIN household.space_members b ON b.space_id=a.space_id WHERE a.user_id=$1 AND b.user_id=$2 AND a.space_kind='family' AND b.space_kind='family')))`, user, target).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return platform.NotFound()
		}
		return nil
	case "space":
		_, err := s.db.LockMember(ctx, tx, target, user)
		return err
	case "recipe":
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM recipe.recipes r WHERE (r.id=$1 OR r.image_object_id=$2) AND r.deleted_at IS NULL AND r.status='published' AND (r.kind='template' OR EXISTS(SELECT 1 FROM household.space_members m WHERE m.space_id=r.space_id AND m.user_id=$3)))`, target, id, user).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return platform.NotFound()
		}
		return nil
	case "food":
		var scope string
		var sid *string
		err := tx.QueryRow(ctx, "SELECT scope,owner_space_id::text FROM catalog.foods WHERE id=$1 AND status!='inactive' FOR SHARE", target).Scan(&scope, &sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		if scope == "global" {
			return nil
		}
		if sid == nil {
			return platform.NotFound()
		}
		_, err = s.db.LockMember(ctx, tx, *sid, user)
		return err
	default:
		return platform.NotFound()
	}
}
func (s *Service) Read(ctx context.Context, user, id string) (Download, error) {
	var key, purpose, target string
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "SELECT object_key,purpose,target_id::text FROM media.objects WHERE id=$1 AND status='ready' FOR SHARE", id).Scan(&key, &purpose, &target)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		return s.canRead(ctx, tx, user, purpose, target, id)
	})
	if err != nil {
		return Download{}, err
	}
	url, err := s.store.DownloadURL(ctx, key, "", 15*time.Minute)
	if err != nil {
		return Download{}, err
	}
	return Download{url, s.db.Clock.Now().Add(15 * time.Minute)}, nil
}
