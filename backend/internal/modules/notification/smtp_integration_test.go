//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func mailpitMessage(t *testing.T, recipient string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, _ := http.NewRequestWithContext(ctx, "GET", testutil.MailpitURL()+"/api/v1/search?query="+url.QueryEscape("to:"+recipient), nil)
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("real Mailpit HTTP required: %v", err)
		}
		var list struct {
			Messages []struct {
				ID string `json:"ID"`
			} `json:"messages"`
		}
		err = json.NewDecoder(response.Body).Decode(&list)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("Mailpit search %d %v", response.StatusCode, err)
		}
		if len(list.Messages) > 0 {
			request, _ = http.NewRequestWithContext(ctx, "GET", testutil.MailpitURL()+"/api/v1/message/"+list.Messages[0].ID, nil)
			response, err = client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			var message map[string]any
			err = json.NewDecoder(response.Body).Decode(&message)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			return message
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("SMTP message not visible in real Mailpit mailbox")
		}
	}
}

// Real SMTP acceptance plus a separately read mailbox must preserve the generated OTP, Unicode subject, and safe address headers.
func TestRealMailpitSMTPAndWorkerEmailPreserveVietnamese(t *testing.T) {
	recipient := platform.ID() + "@test.local"
	smtp := SMTP{Addr: testutil.SMTPAddr(), From: "Đi chợ tiện lợi <no-reply@test.local>"}
	s := New(nil, smtp, nil)
	payload, _ := json.Marshal(map[string]string{"to": recipient, "purpose": "register", "code": "317942"})
	if err := s.Handle(context.Background(), platform.Event{Event: "email", Payload: payload}); err != nil {
		t.Fatalf("real Mailpit SMTP required: %v", err)
	}
	message := mailpitMessage(t, recipient)
	if message["Subject"] != "Đi chợ tiện lợi — xác thực tài khoản" || !strings.Contains(fmt.Sprint(message["Text"]), "Mã xác thực của bạn: 317942") {
		t.Fatalf("SMTP OTP content mismatch subject=%v text=%v", message["Subject"], message["Text"])
	}
	recipient = platform.ID() + "@test.local"
	subject := "Báo cáo đã sẵn sàng — gia đình"
	if err := smtp.Send(context.Background(), "Người nhận <"+recipient+">", subject, "Món ăn từ rau củ\nKhông thêm người nhận."); err != nil {
		t.Fatal(err)
	}
	message = mailpitMessage(t, recipient)
	if message["Subject"] != subject || !strings.Contains(fmt.Sprint(message["Text"]), "Món ăn từ rau củ") {
		t.Fatalf("SMTP Unicode mismatch %v", message["Subject"])
	}
}

// A transport configured to require TLS must not transmit an email through a plaintext-only SMTP server.
func TestMailpitSMTPRequiredTLSFailsClosed(t *testing.T) {
	err := (SMTP{Addr: testutil.SMTPAddr(), From: "sender@test.local", RequireTLS: true}).Send(context.Background(), platform.ID()+"@test.local", "Subject", "private-body")
	if err == nil || strings.Contains(err.Error(), "private-body") {
		t.Fatalf("SMTP TLS policy %v", err)
	}
}
