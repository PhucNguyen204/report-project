package app

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRuntimeConfigurationRejectsUnsafeAIModels(t *testing.T) {
	for _, model := range []string{"bad model", "gpt\nsecret", "https://secret.test/model", "unsupported:model", strings.Repeat("a", 129)} {
		env := developmentEnv()
		env["OPENAI_MODEL"] = model
		_, err := LoadRuntimeConfig(func(k string) string { return env[k] })
		if err == nil || strings.Contains(err.Error(), model) {
			t.Fatal("unsafe model accepted or echoed")
		}
	}
}

func TestAIKeyReadsOnlyPrivateBoundedRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.key")
	key := "local-fixture-provider-token-1234567890"
	if err := os.WriteFile(path, []byte(key+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadOpenAIKey(path)
	if err != nil || got != key {
		t.Fatal("private token was not read")
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	if got, err = ReadOpenAIKey(path); err != nil || got != key {
		t.Fatal("read-only private token was rejected")
	}
	for _, mode := range []os.FileMode{0644, 0640, 0000, 0700} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadOpenAIKey(path); err == nil || strings.Contains(err.Error(), key) {
			t.Fatal("unsafe permissions accepted or token leaked")
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "a", "token with spaces1234567890", "token-with\nnewline-1234567890", "token-with-\x001234567890", strings.Repeat("a", 4097)} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadOpenAIKey(path); err == nil || strings.Contains(err.Error(), path) {
			t.Fatal("invalid token accepted or path leaked")
		}
	}
	for _, invalid := range []string{t.TempDir(), path + ".missing"} {
		if _, err := ReadOpenAIKey(invalid); err == nil {
			t.Fatal("non-file accepted")
		}
	}
	if got, err := ReadOpenAIKey(""); err != nil || got != "" {
		t.Fatal("optional provider did not remain disabled")
	}
}

func TestAIKeyNamedPipeDoesNotBlockStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.pipe")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadOpenAIKey(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("named pipe accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("credential reader blocked on named pipe")
	}
}

func TestAIConfigurationKeepsExplicitModelAndOptionalKeyPath(t *testing.T) {
	env := developmentEnv()
	cfg, err := LoadRuntimeConfig(func(k string) string { return env[k] })
	if err != nil || cfg.OpenAIKeyFile != "" || cfg.OpenAIModel != "gpt-5.6-terra" {
		t.Fatal("optional provider defaults invalid")
	}
	env["OPENAI_MODEL"], env["OPENAI_API_KEY_FILE"] = "configured-model-v1", ".local/provider.key"
	cfg, err = LoadRuntimeConfig(func(k string) string { return env[k] })
	if err != nil || cfg.OpenAIModel != "configured-model-v1" || cfg.OpenAIKeyFile != ".local/provider.key" {
		t.Fatal("explicit provider config lost")
	}
}
