package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSingleDatabaseConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.MaxOpenConns != 20 {
		t.Fatalf("unexpected pool default: %d", cfg.Database.MaxOpenConns)
	}
	if cfg.OrderNotify.Workers != 4 || cfg.OrderNotify.IdleInterval != "500ms" {
		t.Fatalf("unexpected order notify defaults: %+v", cfg.OrderNotify)
	}
}

func TestRemovedFeatureSwitchesRejected(t *testing.T) {
	for _, extra := range []string{
		"order:\n  enabled: true\n",
		"payment:\n  allowUnverifiedNonProd: true\n",
		"gameNotify:\n  allowedHosts: []\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		data := "database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\n" + extra
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("obsolete switch accepted: %q", extra)
		}
	}
}

func TestLegacyOrderDatabaseConfigurationRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\norderDatabase:\n  dsn: 'old-connection'\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "orderDatabase") {
		t.Fatalf("expected obsolete orderDatabase rejection, got %v", err)
	}
}

func TestMigratedEventReportURLRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center'\n" +
		"platform:\n  eventReportURL: 'https://openapiv3.qk6.top/report/event'\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("旧 platform.eventReportURL 已迁移到 sdk-service，不应被 SDK Core 接受")
	}
}

func TestAdminSecretConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "database:\n  dsn: test-dsn\nadminAuth:\n  secret: file-secret-0123456789abcdef0123456789\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAuth.Secret != "file-secret-0123456789abcdef0123456789" {
		t.Fatal("YAML 管理密钥加载失败")
	}
	t.Setenv("ADMIN_AUTH_SECRET", "env-secret-0123456789abcdef0123456789")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminAuth.Secret != "env-secret-0123456789abcdef0123456789" {
		t.Fatal("管理密钥环境变量覆盖失败")
	}
}

func TestRemovedAdminAuthEnabledRejected(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		data := "database:\n  dsn: test-dsn\nadminAuth:\n  enabled: " + enabled + "\n"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("removed adminAuth.enabled=%s should be rejected", enabled)
		}
	}
}

// TestOrderNotifyConfiguration 只允许调整 Worker 和目标并发，其余使用固定默认值。
func TestOrderNotifyConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		workers int
		host    int
		wantErr bool
	}{
		{name: "custom", yaml: "orderNotify:\n  workers: 8\n  hostConcurrency: 2\n", workers: 8, host: 2},
		{name: "one worker", yaml: "orderNotify:\n  workers: 1\n  hostConcurrency: 0\n", workers: 1, host: 0},
		{name: "maximum", yaml: "orderNotify:\n  workers: 32\n  hostConcurrency: 1\n", workers: 32, host: 1},
		{name: "zero workers", yaml: "orderNotify:\n  workers: 0\n", wantErr: true},
		{name: "too many workers", yaml: "orderNotify:\n  workers: 33\n", wantErr: true},
		{name: "negative host limit", yaml: "orderNotify:\n  hostConcurrency: -1\n", wantErr: true},
		{name: "host limit too large", yaml: "orderNotify:\n  workers: 4\n  hostConcurrency: 4\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			data := "database:\n  dsn: test-dsn\n" + tc.yaml
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected invalid order notify config: %q", tc.yaml)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OrderNotify.Workers != tc.workers || cfg.OrderNotify.HostConcurrency != tc.host ||
				cfg.OrderNotify.IdleInterval != "500ms" {
				t.Fatalf("unexpected order notify config: %+v", cfg.OrderNotify)
			}
		})
	}
}

// Strict YAML parsing must reject retired knobs so old production files are migrated before rollout.
func TestSlimConfigurationRejectsRetiredKeys(t *testing.T) {
	for _, key := range []string{
		"database:\n  maxIdleConns: 10\n",
		"database:\n  connMaxLifetime: 30m\n",
		"orderNotify:\n  idleInterval: 500ms\n",
		"orderNotify:\n  disableKeepAlives: false\n",
		"orderNotify:\n  maxIdleConns: 32\n",
		"configCache:\n  maxInflight: 8\n",
		"server:\n  requestTimeout: 5s\n",
		"log:\n  format: json\n",
		"log:\n  slowThreshold: 500ms\n",
	} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			data := "database:\n  dsn: test-dsn\n" + key
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatalf("retired option accepted: %s", key)
			}
		})
	}
}

func TestMaxOpenConnsRemainsConfigurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "database:\n  dsn: test-dsn\n  maxOpenConns: 24\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Database.MaxOpenConns != 24 || cfg.Database.MaxIdleConns != 10 {
		t.Fatalf("database defaults and maxOpenConns: %+v %v", cfg.Database, err)
	}
}
