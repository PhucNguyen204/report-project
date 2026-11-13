package app

import (
	"context"
	"crypto/ed25519"
	"dicho/internal/modules/accounts"
	"dicho/internal/modules/exports"
	"dicho/internal/modules/inventory"
	"dicho/internal/modules/media"
	"dicho/internal/modules/notification"
	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"net/http"
	"time"
)

type Options struct {
	SigningKey  ed25519.PrivateKey
	QueueKey    []byte
	MediaStore  *media.S3Store
	ExportStore exports.Store
	Email       notification.EmailSender
	Push        notification.PushSender
	HTTP        Config
}
type System struct {
	DB            *platform.DB
	Accounts      *accounts.Service
	Handler       http.Handler
	Mux           *http.ServeMux
	Worker        *river.Client[pgx.Tx]
	Dispatcher    *Dispatcher
	Media         *media.Service
	Exports       *exports.Service
	Notifications *notification.Service
}

func NewSystem(ctx context.Context, db *platform.DB, opts Options) (*System, error) {
	if db == nil || db.Pool == nil || len(opts.SigningKey) != ed25519.PrivateKeySize || len(opts.QueueKey) != 32 || opts.MediaStore == nil || opts.ExportStore == nil {
		return nil, errors.New("database, storage and valid signing/queue keys are required")
	}
	a := accounts.New(db, opts.SigningKey)
	i := inventory.New(db, a)
	p := planning.New(db, i, i, a)
	m := media.New(db, opts.MediaStore)
	e := exports.New(db, p, opts.ExportStore)
	n := notification.New(db, opts.Email, opts.Push)
	db.MemberLeave = i.OnMemberLeave
	db.RecipeDelete = p.OnRecipeDelete
	a.ValidateMedia = m.Validate
	p.ValidateMedia = m.Validate
	dispatch := &Dispatcher{DB: db, Accounts: a, Inventory: i, Planning: p, Media: m, Exports: e, Notifications: n}
	queue, err := platform.ConfigureQueue(ctx, db, dispatch.Handle, opts.QueueKey)
	if err != nil {
		return nil, err
	}
	s := &System{DB: db, Accounts: a, Worker: queue, Dispatcher: dispatch, Media: m, Exports: e, Notifications: n, Mux: http.NewServeMux()}
	opts.HTTP.Mux = s.Mux
	s.Handler = NewHandler(db, a.Authenticate, opts.HTTP, a.Register, a.RegisterCatalog, a.RegisterAdmin, a.RegisterProfile, a.RegisterGroups, a.RegisterPrivacy, i.Register, p.Register, m.Register, e.Register, n.Register)
	return s, nil
}
func (s *System) Tick(ctx context.Context) error {
	return s.DB.Tx(ctx, func(tx pgx.Tx) error {
		now := s.DB.Clock.Now()
		tag, err := tx.Exec(ctx, "INSERT INTO platform.scheduler_runs(slot) VALUES($1) ON CONFLICT DO NOTHING", now.Truncate(time.Minute))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		joined := platform.WithTransaction(ctx, tx)
		for _, cleanup := range []func(context.Context) error{s.Notifications.DispatchReminders, s.Accounts.Cleanup, s.Notifications.Cleanup, s.Media.Cleanup, s.Exports.Cleanup} {
			if err = cleanup(joined); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, "DELETE FROM platform.idempotency_keys WHERE created_at<$1", now.Add(-7*24*time.Hour)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM platform.scheduler_runs WHERE slot<$1", now.Add(-7*24*time.Hour))
		return err
	})
}
func (s *System) RunScheduler(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
