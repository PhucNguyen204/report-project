package platform

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (db *DB) LockActor(ctx context.Context, tx pgx.Tx, user string) (string, error) {
	return db.actor(ctx, tx, user, true)
}

func (db *DB) CheckActor(ctx context.Context, q Querier, user string) (string, error) {
	return db.actor(ctx, q, user, false)
}

func (db *DB) actor(ctx context.Context, q Querier, user string, lock bool) (string, error) {
	p, _ := ctx.Value(principalKey{}).(Principal)
	session := p.UserID == user && p.SessionID != ""
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	var role string
	var version int64
	err := q.QueryRow(ctx, "SELECT system_role,token_version FROM identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL"+suffix, user).Scan(&role, &version)
	invalid := func() error { return Err(401, 3001, "Phiên không còn hiệu lực") }
	if err != nil {
		if session && errors.Is(err, pgx.ErrNoRows) {
			return "", invalid()
		}
		return "", err
	}
	if !session {
		return role, nil
	}
	if role != p.SystemRole || version != p.TokenVersion {
		return "", invalid()
	}
	var id string
	err = q.QueryRow(ctx, "SELECT id::text FROM identity.refresh_sessions WHERE user_id=$1 AND family_id=$2 AND revoked_at IS NULL AND rotated_at IS NULL AND expires_at>$3 ORDER BY id LIMIT 1"+suffix, user, p.SessionID, db.Clock.Now()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", invalid()
	}
	return role, err
}
