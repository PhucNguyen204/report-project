package testutil

import "os"

func service(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func S3Endpoint() string { return service("TEST_S3_ENDPOINT", "http://127.0.0.1:59000") }
func SMTPAddr() string   { return service("TEST_SMTP_ADDR", "127.0.0.1:51025") }
func MailpitURL() string { return service("TEST_MAILPIT_URL", "http://127.0.0.1:58025") }
