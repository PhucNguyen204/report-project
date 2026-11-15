package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"dicho/internal/observability"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type RuntimeConfig struct {
	Development                                                               bool
	AutoMigrate                                                               bool
	Mode, ListenAddr, DatabaseURL, RedisURL                                   string
	OpsListenAddr, TraceEndpoint                                              string
	SigningKeyFile, QueueKeyFile                                              string
	S3Endpoint, S3Region, S3AccessKey, S3SecretKey, MediaBucket, ExportBucket string
	S3PublicEndpoint                                                          string
	SMTPAddr, SMTPFrom, SMTPUser, SMTPPassword                                string
	FCMProject, FCMCredentialFile, MinimumAppVersion                          string
	OpenAIKeyFile, OpenAIModel                                                string
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
	c.AutoMigrate = c.Development
	if raw := get("AUTO_MIGRATE"); raw != "" {
		var err error
		c.AutoMigrate, err = strconv.ParseBool(raw)
		if err != nil {
			return c, errors.New("invalid AUTO_MIGRATE")
		}
	}
	if !c.Development && c.AutoMigrate {
		return c, errors.New("production requires a separate dicho-migrate process")
	}
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
	c.OpsListenAddr = value("OPS_LISTEN_ADDR", "127.0.0.1:8081")
	_, port, err := net.SplitHostPort(c.OpsListenAddr)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || n <= 0 || n > 65535 {
		return c, errors.New("invalid OPS_LISTEN_ADDR")
	}
	c.TraceEndpoint = get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	if err := observability.ValidateTraceEndpoint(c.TraceEndpoint); err != nil {
		return c, err
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
	c.S3PublicEndpoint = value("S3_PUBLIC_ENDPOINT", c.S3Endpoint)
	if !validURL(c.S3PublicEndpoint, "http", "https") {
		return c, errors.New("invalid S3_PUBLIC_ENDPOINT")
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
	c.OpenAIKeyFile = get("OPENAI_API_KEY_FILE")
	c.OpenAIModel = value("OPENAI_MODEL", "gpt-5.6-terra")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(c.OpenAIModel) {
		return c, errors.New("invalid OPENAI_MODEL")
	}
	c.MinimumAppVersion = value("MIN_APP_VERSION", "1.0.0")
	if _, err := compareVersion(c.MinimumAppVersion, c.MinimumAppVersion); err != nil {
		return c, errors.New("invalid MIN_APP_VERSION")
	}
	if !c.Development && (!strings.HasPrefix(c.S3Endpoint, "https://") || !strings.HasPrefix(c.S3PublicEndpoint, "https://") || c.FCMProject == "") {
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
