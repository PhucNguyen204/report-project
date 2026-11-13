package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type RuntimeConfig struct {
	Development                                                               bool
	Mode, ListenAddr, DatabaseURL, RedisURL                                   string
	SigningKeyFile, QueueKeyFile                                              string
	S3Endpoint, S3Region, S3AccessKey, S3SecretKey, MediaBucket, ExportBucket string
	SMTPAddr, SMTPFrom, SMTPUser, SMTPPassword                                string
	FCMProject, FCMCredentialFile, MinimumAppVersion                          string
	TrustedProxies                                                            []net.IPNet
}

func LoadRuntimeConfig(get func(string) string) (RuntimeConfig, error) {
	c := RuntimeConfig{}
	env := get("APP_ENV")
	if env == "" {
		env = "production"
	}
	if env != "development" && env != "production" {
		return c, errors.New("APP_ENV must be development or production")
	}
	c.Development = env == "development"
	value := func(key, fallback string) string {
		if v := get(key); v != "" {
			return v
		}
		return fallback
	}
	listen, mode := "0.0.0.0:8080", "api"
	if c.Development {
		listen, mode = "127.0.0.1:8080", "all"
	}
	c.ListenAddr = value("LISTEN_ADDR", listen)
	c.Mode = value("MODE", mode)
	if c.Mode != "api" && c.Mode != "worker" && c.Mode != "all" {
		return c, errors.New("MODE must be api, worker or all")
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return c, errors.New("invalid LISTEN_ADDR")
	}
	for _, item := range []struct {
		key string
		dst *string
	}{{"DATABASE_URL", &c.DatabaseURL}, {"REDIS_URL", &c.RedisURL}, {"S3_ENDPOINT", &c.S3Endpoint}, {"S3_ACCESS_KEY", &c.S3AccessKey}, {"S3_SECRET_KEY", &c.S3SecretKey}, {"SIGNING_KEY_FILE", &c.SigningKeyFile}, {"QUEUE_KEY_FILE", &c.QueueKeyFile}, {"SMTP_ADDR", &c.SMTPAddr}, {"SMTP_FROM", &c.SMTPFrom}} {
		*item.dst = get(item.key)
		if *item.dst == "" {
			return c, fmt.Errorf("%s is required", item.key)
		}
	}
	validURL := func(raw string, schemes ...string) bool {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return false
		}
		for _, scheme := range schemes {
			if u.Scheme == scheme {
				return true
			}
		}
		return false
	}
	if !validURL(c.DatabaseURL, "postgres", "postgresql") {
		return c, errors.New("invalid DATABASE_URL")
	}
	if !validURL(c.RedisURL, "redis", "rediss") {
		return c, errors.New("invalid REDIS_URL")
	}
	if !validURL(c.S3Endpoint, "http", "https") {
		return c, errors.New("invalid S3_ENDPOINT")
	}
	if _, _, err := net.SplitHostPort(c.SMTPAddr); err != nil {
		return c, errors.New("invalid SMTP_ADDR")
	}
	if _, err := mail.ParseAddress(c.SMTPFrom); err != nil || strings.ContainsAny(c.SMTPFrom, "\r\n") {
		return c, errors.New("invalid SMTP_FROM")
	}
	c.S3Region = value("S3_REGION", "us-east-1")
	c.MediaBucket = value("MEDIA_BUCKET", "media")
	c.ExportBucket = value("EXPORT_BUCKET", "exports")
	c.SMTPUser = get("SMTP_USER")
	c.SMTPPassword = get("SMTP_PASSWORD")
	c.FCMProject = get("FCM_PROJECT_ID")
	c.FCMCredentialFile = get("FCM_CREDENTIALS_FILE")
	c.MinimumAppVersion = value("MIN_APP_VERSION", "1.0.0")
	if _, err := compareVersion(c.MinimumAppVersion, c.MinimumAppVersion); err != nil {
		return c, errors.New("invalid MIN_APP_VERSION")
	}
	if !c.Development && (!strings.HasPrefix(c.S3Endpoint, "https://") || c.FCMProject == "") {
		return c, errors.New("production requires HTTPS object storage and FCM_PROJECT_ID")
	}
	for _, raw := range strings.Split(get("TRUSTED_PROXIES"), ",") {
		if raw = strings.TrimSpace(raw); raw != "" {
			_, network, err := net.ParseCIDR(raw)
			if err != nil {
				return c, errors.New("invalid TRUSTED_PROXIES")
			}
			c.TrustedProxies = append(c.TrustedProxies, *network)
		}
	}
	return c, nil
}
func ReadRuntimeKeys(signingPath, queuePath string) (ed25519.PrivateKey, []byte, error) {
	read := func(path string, n int) ([]byte, error) {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("secret key must be a readable private regular file (0600)")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("cannot read secret key")
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(b) != n {
			return nil, errors.New("secret key has invalid format or length")
		}
		return b, nil
	}
	signing, err := read(signingPath, ed25519.PrivateKeySize)
	if err != nil {
		return nil, nil, err
	}
	queue, err := read(queuePath, 32)
	if err != nil {
		return nil, nil, err
	}
	return ed25519.PrivateKey(signing), queue, nil
}
func WriteLocalKeys(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for _, item := range []struct {
		name string
		n    int
	}{{"signing.key", ed25519.PrivateKeySize}, {"queue.key", 32}} {
		path := filepath.Join(dir, item.name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		var data []byte
		if item.n == ed25519.PrivateKeySize {
			_, data, err = ed25519.GenerateKey(rand.Reader)
		} else {
			data = make([]byte, item.n)
			_, err = rand.Read(data)
		}
		if err == nil {
			_, err = file.WriteString(base64.StdEncoding.EncodeToString(data) + "\n")
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	_, _, err := ReadRuntimeKeys(filepath.Join(dir, "signing.key"), filepath.Join(dir, "queue.key"))
	return err
}
