package notification

import (
	"context"
	"dicho/internal/platform"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func (s *Service) DispatchReminders(ctx context.Context) error {
	today := s.db.Today()
	now := s.db.Clock.Now().In(today.Location())
	runAt := "08:00"
	days, maxLots := 3, 5
	if err := s.db.Pool.QueryRow(ctx, "SELECT value #>> '{}' FROM admin.system_configs WHERE key='reminder.runAt'").Scan(&runAt); err != nil {
		return err
	}
	if err := s.db.Pool.QueryRow(ctx, "SELECT (value #>> '{}')::int FROM admin.system_configs WHERE key='reminder.defaultDays'").Scan(&days); err != nil {
		return err
	}
	if err := s.db.Pool.QueryRow(ctx, "SELECT (value #>> '{}')::int FROM admin.system_configs WHERE key='reminder.maxLotsPerNotice'").Scan(&maxLots); err != nil {
		return err
	}
	run, err := time.Parse("15:04", runAt)
	if err != nil || days < 1 || days > 7 || maxLots < 1 || maxLots > 10 {
		return platform.Invalid("Cấu hình nhắc hạn không hợp lệ")
	}
	if now.Hour()*60+now.Minute() < run.Hour()*60+run.Minute() {
		return nil
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT u.id::text,f.name FROM identity.users u JOIN identity.user_settings st ON st.user_id=u.id JOIN household.space_members m ON m.user_id=u.id JOIN household.spaces sp ON sp.id=m.space_id JOIN pantry.food_lots l ON l.space_id=m.space_id JOIN catalog.foods f ON f.id=l.food_id WHERE u.status='active' AND u.deleted_at IS NULL AND st.expiry_notify_on AND sp.dissolved_at IS NULL AND l.lifecycle='active' AND l.deleted_at IS NULL AND l.expires_on BETWEEN $1::date-1 AND $1::date+coalesce(st.reminder_days,$2) ORDER BY u.id,l.expires_on,l.id`, today, days)
	if err != nil {
		return err
	}
	type group struct {
		count int
		names []string
	}
	groups := map[string]*group{}
	for rows.Next() {
		var uid, name string
		if err = rows.Scan(&uid, &name); err != nil {
			rows.Close()
			return err
		}
		g := groups[uid]
		if g == nil {
			g = &group{}
			groups[uid] = g
		}
		g.count++
		if len(g.names) < maxLots {
			g.names = append(g.names, name)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		for uid, g := range groups {
			body := fmt.Sprintf("Có %d lô thực phẩm sắp hoặc vừa hết hạn: %s", g.count, strings.Join(g.names, ", "))
			if g.count > len(g.names) {
				body += fmt.Sprintf(" và %d lô khác", g.count-len(g.names))
			}
			if err := s.db.Enqueue(ctx, tx, "notification", "reminder:"+uid+":"+today.Format("2006-01-02"), notice{UserID: uid, Title: "Nhắc hạn thực phẩm", Body: body, Type: "expiry"}, s.db.Clock.Now()); err != nil {
				return err
			}
		}
		return nil
	})
}
