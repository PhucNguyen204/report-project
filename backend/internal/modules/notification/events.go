package notification

import (
	"context"
	"dicho/internal/platform"
	"net/mail"
	"strings"
	"unicode/utf8"
)

type emailEvent struct{ To, Purpose, Code, Subject, Body string }
type PushMessage struct {
	Token string
	Title string
	Body  string
	Data  map[string]string
}
type messageSender interface {
	SendMessage(context.Context, PushMessage) error
}

func providerFailure(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return platform.Err(503, 5003, message)
}
func validateEmail(p emailEvent) error {
	if strings.ContainsAny(p.To+p.Subject, "\r\n\x00") {
		return platform.Invalid("Nội dung thư không hợp lệ")
	}
	if _, err := mail.ParseAddress(p.To); err != nil {
		return platform.Invalid("Địa chỉ thư không hợp lệ")
	}
	if p.Body == "" && p.Code == "" {
		return platform.Invalid("Thiếu nội dung thư")
	}
	if p.Code != "" {
		if len(p.Code) != 6 {
			return platform.Invalid("Mã xác thực không hợp lệ")
		}
		for _, r := range p.Code {
			if r < '0' || r > '9' {
				return platform.Invalid("Mã xác thực không hợp lệ")
			}
		}
	}
	return nil
}
func validateNotice(p *notice, key string) error {
	if !validID(p.UserID) || key == "" || strings.TrimSpace(p.Title) == "" || utf8.RuneCountInString(p.Title) > 200 || utf8.RuneCountInString(p.Body) > 4000 {
		return platform.Invalid("Thông báo không hợp lệ")
	}
	if p.Type == "" {
		p.Type = "activity"
	}
	if p.Type != "activity" && p.Type != "assignment" && p.Type != "expiry" {
		return platform.Invalid("Loại thông báo không hợp lệ")
	}
	for _, id := range []string{p.SpaceID, p.EntityID} {
		if id != "" && !validID(id) {
			return platform.Invalid("Đối tượng thông báo không hợp lệ")
		}
	}
	for key, value := range p.Data {
		if key == "" || len(key) > 100 || len(value) > 1024 || key == "from" || strings.HasPrefix(key, "google.") || strings.HasPrefix(key, "gcm.") {
			return platform.Invalid("Dữ liệu thông báo không hợp lệ")
		}
	}
	return nil
}
