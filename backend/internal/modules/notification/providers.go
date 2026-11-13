package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type SMTP struct {
	Addr, From, User, Password string
	RequireTLS                 bool
}

func (s SMTP) Send(ctx context.Context, to, subject, body string) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			var app *platform.Error
			if !errors.As(err, &app) {
				err = providerFailure(ctx, "Dịch vụ thư điện tử tạm thời không khả dụng")
			}
		}
	}()
	if strings.ContainsAny(subject+to+s.From, "\r\n\x00") {
		return platform.Invalid("Nội dung thư không hợp lệ")
	}
	recipient, parseErr := mail.ParseAddress(to)
	if parseErr != nil {
		return platform.Invalid("Địa chỉ thư không hợp lệ")
	}
	sender, parseErr := mail.ParseAddress(s.From)
	if parseErr != nil {
		return platform.Invalid("Địa chỉ gửi không hợp lệ")
	}
	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return err
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	} else if s.RequireTLS {
		return errors.New("SMTP TLS required")
	}
	if s.User != "" {
		if err = client.Auth(smtp.PlainAuth("", s.User, s.Password, host)); err != nil {
			return err
		}
	}
	if err = client.Mail(sender.Address); err != nil {
		return err
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return err
	}
	out, err := client.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", sender.String(), recipient.String(), mime.QEncoding.Encode("UTF-8", subject), body)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return client.Quit()
}

type FCM struct {
	Project string
	Client  *http.Client
	BaseURL string
}

func (f FCM) Send(ctx context.Context, token, title, body string) error {
	return f.SendMessage(ctx, PushMessage{Token: token, Title: title, Body: body})
}
func (f FCM) SendMessage(ctx context.Context, message PushMessage) error {
	if f.Project == "" || f.Client == nil {
		return providerFailure(ctx, "Dịch vụ thông báo chưa được cấu hình")
	}
	if message.Token == "" || strings.TrimSpace(message.Title) == "" {
		return platform.Invalid("Thông báo đẩy không hợp lệ")
	}
	base := f.BaseURL
	if base == "" {
		base = "https://fcm.googleapis.com"
	}
	values := map[string]any{"token": message.Token, "notification": map[string]string{"title": message.Title, "body": message.Body}}
	if len(message.Data) > 0 {
		values["data"] = message.Data
	}
	payload, _ := json.Marshal(map[string]any{"message": values})
	if len(payload) > 4096 {
		return platform.Invalid("Thông báo đẩy quá lớn")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/v1/projects/"+url.PathEscape(f.Project)+"/messages:send", bytes.NewReader(payload))
	if err != nil {
		return providerFailure(ctx, "Dịch vụ thông báo tạm thời không khả dụng")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.Client.Do(req)
	if err != nil {
		return providerFailure(ctx, "Dịch vụ thông báo tạm thời không khả dụng")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return providerFailure(ctx, "Dịch vụ thông báo tạm thời không khả dụng")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Details []struct {
					Code string `json:"errorCode"`
				} `json:"details"`
			} `json:"error"`
		}
		if resp.StatusCode == 404 && json.Unmarshal(b, &failure) == nil {
			for _, detail := range failure.Error.Details {
				if detail.Code == "UNREGISTERED" {
					return ErrInvalidDevice
				}
			}
		}
		return providerFailure(ctx, "Dịch vụ thông báo tạm thời không khả dụng")
	}
	var success struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(b, &success) != nil || success.Name == "" {
		return providerFailure(ctx, "Phản hồi dịch vụ thông báo không hợp lệ")
	}
	return nil
}
