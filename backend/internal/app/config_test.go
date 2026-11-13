package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func developmentEnv() map[string]string {
	return map[string]string{"APP_ENV": "development", "DATABASE_URL": "postgres://dicho:local@localhost/dicho?sslmode=disable", "REDIS_URL": "redis://localhost:56379/0", "S3_ENDPOINT": "http://localhost:59000", "S3_ACCESS_KEY": "local", "S3_SECRET_KEY": "local", "SIGNING_KEY_FILE": "signing.key", "QUEUE_KEY_FILE": "queue.key", "SMTP_ADDR": "localhost:51025", "SMTP_FROM": "sender@example.test"}
}
func TestMigrationPolicyRequiresSeparateProductionMigrator(t *testing.T) {
	for _, tt := range []struct {
		env, value string
		want, fail bool
	}{
		{"development", "", true, false}, {"development", "false", false, false},
		{"development", "true", true, false}, {"development", "invalid", false, true},
		{"production", "", false, false}, {"production", "false", false, false},
		{"production", "true", false, true},
	} {
		t.Run(tt.env+"/"+tt.value, func(t *testing.T) {
			env := developmentEnv()
			env["APP_ENV"], env["AUTO_MIGRATE"] = tt.env, tt.value
			env["S3_ENDPOINT"], env["FCM_PROJECT_ID"] = "https://storage.example.test", "fixture"
			cfg, err := LoadRuntimeConfig(func(k string) string { return env[k] })
			if (err != nil) != tt.fail || (err == nil && cfg.AutoMigrate != tt.want) {
				t.Fatalf("migration policy auto%v error%v want%v fail%v", cfg.AutoMigrate, err, tt.want, tt.fail)
			}
		})
	}
}
func TestRuntimeConfigurationRejectsInvalidPublicStorageEndpoint(t *testing.T) {
	for _, endpoint := range []string{"bad", "ftp://storage.example.test"} {
		env := developmentEnv()
		env["S3_PUBLIC_ENDPOINT"] = endpoint
		if _, err := LoadRuntimeConfig(func(k string) string { return env[k] }); err == nil {
			t.Fatalf("invalid public endpoint %s accepted", endpoint)
		}
	}
	env := developmentEnv()
	env["APP_ENV"], env["FCM_PROJECT_ID"] = "production", "test-project"
	env["S3_ENDPOINT"], env["S3_PUBLIC_ENDPOINT"] = "https://internal.example.test", "http://public.example.test"
	if _, err := LoadRuntimeConfig(func(k string) string { return env[k] }); err == nil {
		t.Fatal("production public storage without TLS accepted")
	}
}
func TestRuntimeConfigurationValidatesWithoutLeakingSecrets(t *testing.T) {
	env := developmentEnv()
	cfg, err := LoadRuntimeConfig(func(k string) string { return env[k] })
	if err != nil || !cfg.Development || cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("%+v %v", cfg, err)
	}
	for _, key := range []string{"DATABASE_URL", "REDIS_URL", "S3_ENDPOINT", "SIGNING_KEY_FILE", "QUEUE_KEY_FILE", "SMTP_ADDR"} {
		old := env[key]
		env[key] = ""
		if _, err = LoadRuntimeConfig(func(k string) string { return env[k] }); err == nil {
			t.Fatalf("missing%s accepted", key)
		}
		env[key] = old
	}
	env["APP_ENV"] = "production"
	if _, err = LoadRuntimeConfig(func(k string) string { return env[k] }); err == nil {
		t.Fatal("production without FCM/TLS accepted")
	}
	env["DATABASE_URL"] = "broken-SECRET-password"
	_, err = LoadRuntimeConfig(func(k string) string { return env[k] })
	if err == nil || bytes.Contains([]byte(err.Error()), []byte("SECRET")) {
		t.Fatal("invalid configuration leaked secret")
	}
}
func TestLocalKeysArePrivatePersistentAndNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLocalKeys(dir); err != nil {
		t.Fatal(err)
	}
	signPath, queuePath := filepath.Join(dir, "signing.key"), filepath.Join(dir, "queue.key")
	sign, queue, err := ReadRuntimeKeys(signPath, queuePath)
	if err != nil || len(sign) != 64 || len(queue) != 32 {
		t.Fatalf("invalid keys %d %d %v", len(sign), len(queue), err)
	}
	if err = WriteLocalKeys(dir); err != nil {
		t.Fatal(err)
	}
	again, _, err := ReadRuntimeKeys(signPath, queuePath)
	if err != nil || !bytes.Equal(sign, again) {
		t.Fatal("existing signing key replaced")
	}
	if err = os.Chmod(queuePath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ReadRuntimeKeys(signPath, queuePath); err == nil {
		t.Fatal("world-readable queue key accepted")
	}
}

func TestRuntimeConfigurationRejectsMalformedInfrastructureValues(t *testing.T) {
	for key, value := range map[string]string{
		"APP_ENV": "staging-secret", "MODE": "unknown-secret", "LISTEN_ADDR": "invalid-secret",
		"DATABASE_URL": "https://secret@database.test", "REDIS_URL": "https://secret@cache.test",
		"S3_ENDPOINT": "ftp://secret@storage.test", "S3_PUBLIC_ENDPOINT": "https://",
		"SMTP_ADDR": "invalid-secret", "SMTP_FROM": "bad-secret\r\nBcc: hidden@example.test",
		"MIN_APP_VERSION": "1.x.0", "TRUSTED_PROXIES": "secret-invalid-cidr",
	} {
		t.Run(key, func(t *testing.T) {
			env := developmentEnv()
			env[key] = value
			_, err := LoadRuntimeConfig(func(k string) string { return env[k] })
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("invalid configuration accepted or leaked its value for %s: %v", key, err)
			}
		})
	}
	env := developmentEnv()
	env["TRUSTED_PROXIES"] = "10.0.0.0/8,2001:db8::/32"
	cfg, err := LoadRuntimeConfig(func(k string) string { return env[k] })
	if err != nil || len(cfg.TrustedProxies) != 2 {
		t.Fatalf("valid trusted proxies lost: %v", err)
	}
}

func TestMalformedExistingKeyIsRejectedWithoutReplacement(t *testing.T) {
	for _, value := range []string{"invalid-key-format", "YQ=="} {
		dir := t.TempDir()
		path := filepath.Join(dir, "signing.key")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := WriteLocalKeys(dir); err == nil {
			t.Fatal("invalid persisted signing key accepted")
		}
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != value {
			t.Fatal("existing key was replaced")
		}
	}
}
