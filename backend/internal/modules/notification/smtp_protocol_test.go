package notification

import (
	"bufio"
	"context"
	"dicho/internal/platform"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// SMTP server rejections at each transaction phase must stop delivery and never expose the server's diagnostic text to durable job errors.
func TestSMTPProtocolFailuresAreRedactedAndRetryable(t *testing.T) {
	for _, phase := range []string{"greeting", "auth", "starttls", "mail", "recipient", "data", "body", "quit"} {
		t.Run(phase, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				reply := func(text string) { _, _ = fmt.Fprint(conn, text+"\r\n") }
				if phase == "greeting" {
					reply("421 private-server-secret")
					return
				}
				reply("220 test ESMTP")
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						if phase == "auth" {
							reply("250-test\r\n250 AUTH PLAIN")
						} else if phase == "starttls" {
							reply("250-test\r\n250 STARTTLS")
						} else {
							reply("250 test")
						}
					case strings.HasPrefix(line, "AUTH"):
						reply("535 private-server-secret")
					case strings.HasPrefix(line, "STARTTLS"):
						reply("454 private-server-secret")
					case strings.HasPrefix(line, "MAIL"):
						if phase == "mail" {
							reply("550 private-server-secret")
						} else {
							reply("250 ok")
						}
					case strings.HasPrefix(line, "RCPT"):
						if phase == "recipient" {
							reply("550 private-server-secret")
						} else {
							reply("250 ok")
						}
					case strings.HasPrefix(line, "DATA"):
						if phase == "data" {
							reply("554 private-server-secret")
							continue
						}
						reply("354 continue")
						for {
							part, e := reader.ReadString('\n')
							if e != nil {
								return
							}
							if part == ".\r\n" {
								break
							}
						}
						if phase == "body" {
							reply("554 private-server-secret")
						} else {
							reply("250 accepted")
						}
					case strings.HasPrefix(line, "QUIT"):
						if phase == "quit" {
							reply("500 private-server-secret")
						} else {
							reply("221 bye")
						}
						return
					default:
						reply("500 unexpected")
					}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			sender := SMTP{Addr: listener.Addr().String(), From: "sender@test.local"}
			if phase == "auth" {
				sender.User = "user"
				sender.Password = "password"
			}
			err = sender.Send(ctx, "to@test.local", "Subject", "Body")
			var app *platform.Error
			if !errors.As(err, &app) || app.Status != 503 || strings.Contains(err.Error(), "private-server-secret") {
				t.Fatalf("SMTP phase %s: %v", phase, err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("SMTP server transaction did not terminate")
			}
		})
	}
}

func TestSMTPValidatesAddressesConfigurationAndCanceledContext(t *testing.T) {
	for _, tc := range []struct {
		from, to, addr string
		status         int
	}{{"sender@test.local", "invalid", "127.0.0.1:1", 400}, {"invalid", "to@test.local", "127.0.0.1:1", 400}, {"sender@test.local\r\nBcc:evil@test.local", "to@test.local", "127.0.0.1:1", 400}, {"sender@test.local", "to@test.local", "invalid-address", 503}, {"sender@test.local", "to@test.local", "127.0.0.1:1", 503}} {
		err := (SMTP{Addr: tc.addr, From: tc.from}).Send(context.Background(), tc.to, "Subject", "Body")
		var app *platform.Error
		if !errors.As(err, &app) || app.Status != tc.status {
			t.Fatalf("SMTP input %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SMTP{}).Send(ctx, "to@test.local", "Subject", "Body"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled SMTP %v", err)
	}
}
