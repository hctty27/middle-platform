package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSubscriberSecret = "subscriber-secret-0123456789abcdef0123456789"

func testDefaultYAML(t *testing.T, yaml string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join("configs", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	testDefaultYAML(t, "server:\n  address: ':8089'\ndatabase:\n  dsn: file-dsn\nsubscriber:\n  code: access-file\n  secret: file-subscriber-secret-0123456789abcdef\n")
	t.Setenv("ACCESS_DB_DSN", "root:secret@tcp(127.0.0.1:3306)/access?parseTime=true")
	t.Setenv("CONFIG_SUBSCRIBER_CODE", "access-env")
	t.Setenv("CONFIG_SUBSCRIBER_SECRET", validSubscriberSecret)
	t.Setenv("CONFIG_CENTER_URL", "http://127.0.0.1:8080")
	t.Setenv("CONFIG_CLIENT_CONNECT_TIMEOUT_MS", "2000")
	t.Setenv("CONFIG_CLIENT_READ_TIMEOUT_MS", "15000")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != ":8089" || cfg.ConnectTimeout.Milliseconds() != 2000 ||
		cfg.SubscriberCode != "access-env" || cfg.SubscriberSecret != validSubscriberSecret {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadRequiresDefaultYAML(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("ACCESS_DB_DSN", "test-dsn")
	t.Setenv("CONFIG_SUBSCRIBER_CODE", "access-test")
	t.Setenv("CONFIG_SUBSCRIBER_SECRET", validSubscriberSecret)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "configs/config.yaml") {
		t.Fatalf("缺少默认 YAML 应拒绝启动: %v", err)
	}
}

func TestLoadRejectsMissingSubscriberCode(t *testing.T) {
	testDefaultYAML(t, "database:\n  dsn: file-dsn\nsubscriber:\n  code: ''\n  secret: " + validSubscriberSecret + "\n")
	t.Setenv("ACCESS_DB_DSN", "root:secret@tcp(127.0.0.1:3306)/access")
	t.Setenv("CONFIG_SUBSCRIBER_CODE", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "subscriber.code") {
		t.Fatalf("缺少接入端编码必须拒绝启动: %v", err)
	}
}

func TestLoadRejectsShortSubscriberSecret(t *testing.T) {
	testDefaultYAML(t, "database:\n  dsn: file-dsn\nsubscriber:\n  code: access-test\n  secret: short\n")
	t.Setenv("ACCESS_DB_DSN", "test-dsn")
	t.Setenv("CONFIG_SUBSCRIBER_SECRET", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "subscriber.secret") {
		t.Fatalf("缺少或过短的接入端密钥必须拒绝启动: %v", err)
	}
}

func TestExampleLoggingConfiguration(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "log:\n  level: info\n  accessEnabled: true\n") {
		t.Fatal("config-center-access 示例 YAML 必须显式展示 info 与 accessEnabled=true")
	}
	t.Setenv("ACCESS_DB_DSN", "test-dsn")
	t.Setenv("CONFIG_SUBSCRIBER_CODE", "example-test")
	t.Setenv("CONFIG_SUBSCRIBER_SECRET", validSubscriberSecret)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "info" || !cfg.Log.AccessEnabled {
		t.Fatalf("example logging defaults: %+v", cfg.Log)
	}
}
