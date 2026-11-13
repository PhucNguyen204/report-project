package exports

import (
	"context"
	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type Reporter interface {
	Report(context.Context, string, string, string, time.Time, time.Time, string) (planning.ReportResult, error)
}
type Store interface {
	Put(context.Context, string, string, []byte) error
	DownloadURL(context.Context, string, string, time.Duration) (string, error)
	Delete(context.Context, []string) error
}
type Service struct {
	db      *platform.DB
	reports Reporter
	store   Store
}

func New(db *platform.DB, reports Reporter, store Store) *Service {
	return &Service{db, reports, store}
}

type Input struct {
	ReportType string `json:"reportType"`
	From       string `json:"from"`
	To         string `json:"to"`
	Format     string `json:"format"`
	GroupBy    string `json:"groupBy"`
}
type Result struct {
	ID        string    `json:"exportId"`
	Status    string    `json:"status"`
	URL       string    `json:"downloadUrl,omitempty"`
	Error     string    `json:"error,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func validate(in *Input) (time.Time, time.Time, error) {
	from, e1 := time.Parse("2006-01-02", in.From)
	to, e2 := time.Parse("2006-01-02", in.To)
	if e1 != nil || e2 != nil || to.Before(from) || !to.Before(from.AddDate(0, 12, 0)) {
		return from, to, platform.Invalid("Khoảng báo cáo tối đa12 tháng")
	}
	if in.Format != "pdf" && in.Format != "csv" {
		return from, to, platform.Invalid("Định dạng PDF hoặc CSV")
	}
	if in.ReportType != "spending" && in.ReportType != "consumption" && in.ReportType != "waste" {
		return from, to, platform.Invalid("Loại báo cáo không hợp lệ")
	}
	if in.GroupBy == "" {
		in.GroupBy = "day"
	}
	if in.GroupBy != "day" && in.GroupBy != "week" && in.GroupBy != "month" {
		return from, to, platform.Invalid("Cách nhóm không hợp lệ")
	}
	return from, to, nil
}
func (s *Service) Request(ctx context.Context, user, sid string, in Input) (Result, error) {
	from, to, err := validate(&in)
	if err != nil {
		return Result{}, err
	}
	out := Result{ID: platform.ID(), Status: "pending", ExpiresAt: s.db.Clock.Now().Add(7 * 24 * time.Hour)}
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO report.exports(id,space_id,requested_by,report_type,from_date,to_date,format,group_by,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", out.ID, sid, user, in.ReportType, from, to, in.Format, in.GroupBy, out.ExpiresAt); err != nil {
			return err
		}
		return s.db.Enqueue(ctx, tx, "report.export", out.ID, map[string]string{"exportId": out.ID}, s.db.Clock.Now())
	})
	return out, err
}
func (s *Service) Get(ctx context.Context, user, sid, id string) (Result, error) {
	var out Result
	var key, format string
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockMember(ctx, tx, sid, user); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, "SELECT id::text,status,coalesce(object_key,''),format,error,expires_at FROM report.exports WHERE id=$1 AND space_id=$2 AND requested_by=$3 FOR SHARE", id, sid, user).Scan(&out.ID, &out.Status, &key, &format, &out.Error, &out.ExpiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return platform.NotFound()
		}
		if err != nil {
			return err
		}
		if !s.db.Clock.Now().Before(out.ExpiresAt) {
			return platform.NotFound()
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if out.Status == "ready" {
		expiry := min(15*time.Minute, out.ExpiresAt.Sub(s.db.Clock.Now()))
		if expiry <= 0 {
			return Result{}, platform.NotFound()
		}
		out.URL, err = s.store.DownloadURL(ctx, key, "report-"+id+"."+format, expiry)
	}
	return out, err
}

type request struct {
	user, sid string
	in        Input
	status    string
	expires   time.Time
}

func (s *Service) Process(ctx context.Context, id string) error {
	var r request
	err := s.db.Pool.QueryRow(ctx, "SELECT requested_by::text,space_id::text,report_type,from_date::text,to_date::text,format,group_by,status,expires_at FROM report.exports WHERE id=$1", id).Scan(&r.user, &r.sid, &r.in.ReportType, &r.in.From, &r.in.To, &r.in.Format, &r.in.GroupBy, &r.status, &r.expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.status == "ready" {
		return nil
	}
	fail := func(cause error) error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, dbErr := s.db.Pool.Exec(cleanup, "UPDATE report.exports SET status='failed',error='Không thể tạo tệp báo cáo; hệ thống sẽ thử lại' WHERE id=$1 AND status!='ready'", id)
		return errors.Join(cause, dbErr)
	}
	if !s.db.Clock.Now().Before(r.expires) {
		return fail(platform.NotFound())
	}
	from, to, err := validate(&r.in)
	if err != nil {
		return fail(err)
	}
	report, err := s.reports.Report(ctx, r.user, r.sid, r.in.ReportType, from, to, r.in.GroupBy)
	if err != nil {
		return fail(err)
	}
	var data []byte
	contentType := "text/csv; charset=utf-8"
	if r.in.Format == "pdf" {
		data, err = encodePDF(report)
		contentType = "application/pdf"
	} else {
		data, err = encodeCSV(report)
	}
	if err != nil {
		return fail(err)
	}
	key := "exports/" + id + "/" + platform.ID() + "." + r.in.Format
	if err = s.store.Put(ctx, key, contentType, data); err != nil {
		return fail(errors.Join(err, s.discard(ctx, key)))
	}
	published := false
	err = s.db.Tx(ctx, func(tx pgx.Tx) error {
		published = false
		if _, err := s.db.LockMember(ctx, tx, r.sid, r.user); err != nil {
			return err
		}
		var status string
		var expires time.Time
		if err := tx.QueryRow(ctx, "SELECT status,expires_at FROM report.exports WHERE id=$1 FOR UPDATE", id).Scan(&status, &expires); err != nil {
			return err
		}
		if status == "ready" {
			return nil
		}
		if !s.db.Clock.Now().Before(expires) {
			return platform.NotFound()
		}
		if _, err := tx.Exec(ctx, "UPDATE report.exports SET status='ready',object_key=$2,completed_at=$3,error='' WHERE id=$1", id, key, s.db.Clock.Now()); err != nil {
			return err
		}
		if err := s.db.Enqueue(ctx, tx, "notification", "export.completed:"+id, map[string]string{"userId": r.user, "title": "Báo cáo đã sẵn sàng", "body": "Tệp báo cáo của bạn đã được tạo. Mở báo cáo để tải về.", "type": "activity"}, s.db.Clock.Now()); err != nil {
			return err
		}
		published = true
		return nil
	})
	if err != nil || !published {
		err = errors.Join(err, s.discard(ctx, key))
	}
	if err != nil {
		return fail(err)
	}
	return nil
}
func (s *Service) Handle(ctx context.Context, event platform.Event) error {
	if event.Event == "export.delete" {
		var p struct {
			Keys []string `json:"keys"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}
		return s.store.Delete(ctx, p.Keys)
	}
	if event.Event != "report.export" {
		return errors.New("unsupported export event")
	}
	var payload struct {
		ID string `json:"exportId"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}
	if payload.ID == "" {
		return errors.New("missing export identifier")
	}
	return s.Process(ctx, payload.ID)
}
