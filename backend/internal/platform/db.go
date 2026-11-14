package platform

import (
	"context"
	"crypto/rand"
	"dicho/internal/observability"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type DB struct {
	Pool         *ContextPool
	Clock        Clock
	MemberLeave  MemberLeaveHook
	RecipeDelete RecipeDeleteHook
	EnqueueFunc  func(context.Context, pgx.Tx, string, string, any, time.Time) error
	Observer     *observability.Observer
}

func NewDB(pool *pgxpool.Pool) *DB { return &DB{Pool: &ContextPool{Pool: pool}, Clock: RealClock{}} }
func (db *DB) Today() time.Time {
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	now := db.Clock.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
}
func (db *DB) Tx(ctx context.Context, fn func(pgx.Tx) error) error {
	if tx, ok := ctx.Value(transactionKey{}).(pgx.Tx); ok {
		return fn(tx)
	}
	for attempt := 0; attempt < 3; attempt++ {
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		err = func() error {
			defer tx.Rollback(context.WithoutCancel(ctx))
			if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '3s'; SET LOCAL statement_timeout = '10s'"); err != nil {
				return err
			}
			if err := fn(tx); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}()
		if err == nil {
			return nil
		}
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || (pe.Code != "40P01" && pe.Code != "55P03") {
			return err
		}
		if attempt == 2 {
			return Err(503, 5003, "Dữ liệu đang bận; vui lòng thử lại")
		}
	}
	return nil
}
func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	ms := time.Now().UnixMilli()
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	s := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[:8], s[8:12], s[12:16], s[16:20], s[20:])
}
func (db *DB) RequireMember(ctx context.Context, q Querier, spaceID, userID string) (string, error) {
	role, err := db.CheckActor(ctx, q, userID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "user") {
		return "", NotFound()
	}
	if err != nil {
		return "", err
	}
	err = q.QueryRow(ctx, "SELECT role FROM household.space_members WHERE space_id=$1 AND user_id=$2", spaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", NotFound()
	}
	return role, err
}
func (db *DB) LockMember(ctx context.Context, tx pgx.Tx, spaceID, userID string) (string, error) {
	role, err := db.LockActor(ctx, tx, userID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "user") {
		return "", NotFound()
	}
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(ctx, "SELECT role FROM household.space_members WHERE space_id=$1 AND user_id=$2 FOR SHARE", spaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", NotFound()
	}
	return role, err
}
func (db *DB) Activity(ctx context.Context, tx pgx.Tx, sid, actor, kind, id string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var seq int64
	if err = tx.QueryRow(ctx, "UPDATE household.spaces SET change_seq=change_seq+1 WHERE id=$1 RETURNING change_seq", sid).Scan(&seq); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO platform.activities(space_id,seq,actor_id,kind,entity_id,payload) VALUES($1,$2,$3,$4,$5,$6)", sid, seq, actor, kind, id, b)
	return err
}
func (db *DB) Enqueue(ctx context.Context, tx pgx.Tx, kind, key string, payload any, runAt time.Time) error {
	if db.EnqueueFunc == nil {
		return errors.New("transactional queue is not configured")
	}
	return db.EnqueueFunc(ctx, tx, kind, key, payload, runAt)
}
