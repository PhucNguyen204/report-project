package notification

import (
	"context"
	"dicho/internal/platform"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Item struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Type      string     `json:"type"`
	ReadAt    *time.Time `json:"readAt"`
	CreatedAt time.Time  `json:"createdAt"`
}
type Page struct {
	Items      []Item
	NextCursor string
}
type cursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func validID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	b, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil && len(b) == 16
}
func requireUser(user string) error {
	if user == "" {
		return platform.Err(401, 3001, "Cần đăng nhập")
	}
	if !validID(user) {
		return platform.Invalid("Mã người dùng không hợp lệ")
	}
	return nil
}

func (s *Service) List(ctx context.Context, user string, limit int, encoded string) (Page, error) {
	out := Page{Items: []Item{}}
	if err := requireUser(user); err != nil {
		return out, err
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return out, platform.Invalid("Giới hạn từ1đến100")
	}
	var at *time.Time
	var id *string
	if encoded != "" {
		if len(encoded) > 512 {
			return out, platform.Invalid("Con trỏ không hợp lệ")
		}
		b, err := base64.RawURLEncoding.DecodeString(encoded)
		var cur cursor
		if err != nil || json.Unmarshal(b, &cur) != nil || cur.At.IsZero() || !validID(cur.ID) {
			return out, platform.Invalid("Con trỏ không hợp lệ")
		}
		at, id = &cur.At, &cur.ID
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT id::text,title,body,type,read_at,created_at FROM notification.notifications WHERE user_id=$1 AND ($2::timestamptz IS NULL OR created_at<$2 OR (created_at=$2 AND id>$3::uuid)) ORDER BY created_at DESC,id LIMIT $4`, user, at, id, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Item
		if err = rows.Scan(&item.ID, &item.Title, &item.Body, &item.Type, &item.ReadAt, &item.CreatedAt); err != nil {
			return out, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		if item.ReadAt != nil {
			utc := item.ReadAt.UTC()
			item.ReadAt = &utc
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[len(out.Items)-1]
		b, _ := json.Marshal(cursor{last.CreatedAt, last.ID})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, nil
}
func (s *Service) MarkRead(ctx context.Context, user string, ids []string) error {
	if err := requireUser(user); err != nil {
		return err
	}
	if len(ids) < 1 || len(ids) > 100 {
		return platform.Invalid("Chọn từ1đến100thông báo")
	}
	for _, id := range ids {
		if !validID(id) {
			return platform.Invalid("Mã thông báo không hợp lệ")
		}
	}
	return s.db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.db.LockActor(ctx, tx, user); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE notification.notifications SET read_at=coalesce(read_at,$3) WHERE user_id=$1 AND id=ANY($2::uuid[])", user, ids, s.db.Clock.Now())
		return err
	})
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /it4788/v1/me/notifications", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		limit := 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				return platform.Invalid("Giới hạn từ1đến100")
			}
			limit = n
		}
		page, err := s.List(r.Context(), platform.Current(r).UserID, limit, r.URL.Query().Get("cursor"))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		return json.NewEncoder(w).Encode(map[string]any{"code": 1000, "data": map[string]any{"items": page.Items}, "meta": map[string]any{"nextCursor": page.NextCursor, "serverTime": s.db.Clock.Now().UTC()}})
	}))
	mux.HandleFunc("POST /it4788/v1/me/notifications/read", platform.Handle(func(w http.ResponseWriter, r *http.Request) error {
		if err := requireUser(platform.Current(r).UserID); err != nil {
			return err
		}
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := platform.Decode(r, &in); err != nil {
			return err
		}
		if err := s.MarkRead(r.Context(), platform.Current(r).UserID, in.IDs); err != nil {
			return err
		}
		platform.Write(w, 200, map[string]any{"updated": true})
		return nil
	}))
}
