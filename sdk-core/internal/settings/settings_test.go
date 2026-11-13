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

// TestOrderNotifyConfiguration 验证发货 Worker 参数可以覆盖默认值，并拒绝危险配置。
func TestOrderNotifyConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		workers int
		idle    string
		wantErr bool
	}{
		{name: "custom", yaml: "orderNotify:\n  workers: 8\n  idleInterval: 1s\n", workers: 8, idle: "1s"},
		{name: "minimum", yaml: "orderNotify:\n  workers: 1\n  idleInterval: 100ms\n", workers: 1, idle: "100ms"},
		{name: "maximum", yaml: "orderNotify:\n  workers: 32\n  idleInterval: 10s\n", workers: 32, idle: "10s"},
		{name: "zero workers", yaml: "orderNotify:\n  workers: 0\n", wantErr: true},
		{name: "too many workers", yaml: "orderNotify:\n  workers: 33\n", wantErr: true},
		{name: "too short idle", yaml: "orderNotify:\n  idleInterval: 99ms\n", wantErr: true},
		{name: "too long idle", yaml: "orderNotify:\n  idleInterval: 11s\n", wantErr: true},
		{name: "invalid duration", yaml: "orderNotify:\n  idleInterval: instant\n", wantErr: true},
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
			if cfg.OrderNotify.Workers != tc.workers || cfg.OrderNotify.IdleInterval != tc.idle {
				t.Fatalf("unexpected order notify config: %+v", cfg.OrderNotify)
			}
		})
	}
}
