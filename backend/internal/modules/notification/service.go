package notification

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

type EmailSender interface {
	Send(context.Context, string, string, string) error
}
type PushSender interface {
	Send(context.Context, string, string, string) error
}

var ErrInvalidDevice = errors.New("invalid device token")

type Service struct {
	db    *platform.DB
	email EmailSender
	push  PushSender
}

func New(db *platform.DB, email EmailSender, push PushSender) *Service {
	return &Service{db, email, push}
}

type notice struct {
	UserID   string            `json:"userId"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Type     string            `json:"type"`
	SpaceID  string            `json:"spaceId"`
	EntityID string            `json:"entityId"`
	Data     map[string]string `json:"data"`
}

func (s *Service) Handle(ctx context.Context, e platform.Event) error {
	if e.Event == "email" {
		var p emailEvent
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return platform.Invalid("Thư điện tử không hợp lệ")
		}
		if err := validateEmail(p); err != nil {
			return err
		}
		if s.email == nil {
			return providerFailure(ctx, "Dịch vụ thư điện tử chưa được cấu hình")
		}
		if p.Subject == "" {
			p.Subject = "Đi chợ tiện lợi — xác thực tài khoản"
		}
		if p.Code != "" {
			p.Body = "Mã xác thực của bạn: " + p.Code + ". Không chia sẻ mã này."
		}
		if err := s.email.Send(ctx, p.To, p.Subject, p.Body); err != nil {
			return providerFailure(ctx, "Dịch vụ thư điện tử tạm thời không khả dụng")
		}
		return nil
	}
	if e.Event != "notification" {
		return platform.Invalid("Loại tác vụ thông báo không hợp lệ")
	}
	var p notice
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return platform.Invalid("Thông báo không hợp lệ")
	}
	if err := validateNotice(&p, e.Key); err != nil {
		return err
	}
	var id string
	skip := false
	err := s.db.Tx(ctx, func(tx pgx.Tx) error {
		var active bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL)", p.UserID).Scan(&active); err != nil {
			return err
		}
		if !active {
			skip = true
			return nil
		}
		var enabled bool
		column := "activity_notify_on"
		switch p.Type {
		case "assignment":
			column = "assignment_notify_on"
		case "expiry":
			column = "expiry_notify_on"
		}
		err := tx.QueryRow(ctx, "SELECT "+column+" FROM identity.user_settings WHERE user_id=$1", p.UserID).Scan(&enabled)
		if errors.Is(err, pgx.ErrNoRows) {
			enabled = true
		} else if err != nil {
			return err
		}
		if !enabled {
			skip = true
			return nil
		}
		id = platform.ID()
		tag, err := tx.Exec(ctx, "INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(dedupe_key) DO NOTHING", id, p.UserID, e.Key, p.Title, p.Body, p.Type, s.db.Clock.Now())
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, "SELECT id::text FROM notification.notifications WHERE dedupe_key=$1 AND user_id=$2", e.Key, p.UserID).Scan(&id); err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, "INSERT INTO notification.deliveries(notification_id,device_id) SELECT $1,id FROM identity.devices WHERE user_id=$2 ON CONFLICT DO NOTHING", id, p.UserID)
		return err
	})
	if err != nil || skip {
		return err
	}
	rows, err := s.db.Pool.Query(ctx, "SELECT d.device_id::text,v.fcm_token FROM notification.deliveries d JOIN identity.devices v ON v.id=d.device_id WHERE d.notification_id=$1 AND d.status='pending' ORDER BY d.device_id", id)
	if err != nil {
		return err
	}
	type target struct{ id, token string }
	var targets []target
	for rows.Next() {
		var v target
		if err = rows.Scan(&v.id, &v.token); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var failures []error
	for _, v := range targets {
		if s.push == nil {
			err = providerFailure(ctx, "Dịch vụ thông báo tạm thời không khả dụng")
		} else if sender, ok := s.push.(messageSender); ok {
			data := map[string]string{}
			for k, v := range p.Data {
				data[k] = v
			}
			data["notificationId"], data["type"] = id, p.Type
			if p.SpaceID != "" {
				data["spaceId"] = p.SpaceID
			}
			if p.EntityID != "" {
				data["entityId"] = p.EntityID
			}
			err = sender.SendMessage(ctx, PushMessage{Token: v.token, Title: p.Title, Body: p.Body, Data: data})
		} else {
			err = s.push.Send(ctx, v.token, p.Title, p.Body)
		}
		state := "sent"
		if err != nil {
			state = "pending"
			if errors.Is(err, ErrInvalidDevice) {
				state = "invalid"
			} else {
				failures = append(failures, providerFailure(ctx, "Dịch vụ thông báo tạm thời không khả dụng"))
			}
		}
		var message *string
		if err != nil {
			redacted := "provider delivery failed"
			message = &redacted
		}
		if _, dbErr := s.db.Pool.Exec(ctx, "UPDATE notification.deliveries SET status=$3,attempts=attempts+1,last_error=$4,sent_at=CASE WHEN $3='sent' THEN $5::timestamptz ELSE sent_at END WHERE notification_id=$1 AND device_id=$2", id, v.id, state, message, s.db.Clock.Now()); dbErr != nil {
			return dbErr
		}
		if state == "invalid" {
			if _, dbErr := s.db.Pool.Exec(ctx, "DELETE FROM identity.devices WHERE id=$1", v.id); dbErr != nil {
				return dbErr
			}
		}
	}
	return errors.Join(failures...)
}
