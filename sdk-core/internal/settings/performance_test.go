package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadPerformanceConfig(t *testing.T, extra string) (Config, error) {
	t.Helper()
	t.Setenv("SDK_CORE_DB_DSN", "test-dsn")
	p := filepath.Join(t.TempDir(), "config.yaml")
	data := "database:\n  dsn: test-dsn\n" + extra
	if strings.HasPrefix(extra, "database:\n") {
		data = "database:\n  dsn: test-dsn\n" + strings.TrimPrefix(extra, "database:\n")
	}
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}
func TestPerformanceDefaultsAndRollback(t *testing.T) {
	c, err := loadPerformanceConfig(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ConfigCache.TTL != "5s" || c.ConfigCache.MaxEntries != 4096 || c.ConfigCache.MaxInflight != 8 || c.Server.RequestTimeout != "5s" || c.OrderNotify.HostConcurrency != 1 || c.OrderNotify.DisableKeepAlives {
		t.Fatalf("defaults: %+v", c)
	}
	c, err = loadPerformanceConfig(t, "configCache:\n  ttl: 0s\norderNotify:\n  workers: 1\n  hostConcurrency: 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.ConfigCache.TTL != "0s" || c.OrderNotify.HostConcurrency != 0 || c.OrderNotify.DisableKeepAlives {
		t.Fatalf("rollback: %+v", c)
	}
}
func TestExamplePerformanceConfiguration(t *testing.T) {
	t.Setenv("SDK_CORE_DB_DSN", "test-dsn")
	c, err := Load(filepath.Join("..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ConfigCache.TTL != "5s" || c.OrderNotify.HostConcurrency != 1 || c.Server.RequestTimeout != "5s" {
		t.Fatalf("example drift: %+v", c)
	}
}
func TestPerformanceInvalidConfigurations(t *testing.T) {
	for _, extra := range []string{
		"configCache:\n  ttl: 6s\n", "configCache:\n  maxInflight: 0\n", "configCache:\n  unknown: true\n",
		"server:\n  requestTimeout: 0s\n", "server:\n  requestTimeout: 11s\n",
		"orderNotify:\n  hostConcurrency: -1\n", "orderNotify:\n  hostConcurrency: 4\n", "orderNotify:\n  maxIdleConns: 0\n",
		"orderNotify:\n  maxIdleConnsPerHost: 33\n", "orderNotify:\n  maxConnsPerHost: 0\n", "orderNotify:\n  idleConnTimeout: bad\n",
	} {
		t.Run(extra, func(t *testing.T) {
			if _, err := loadPerformanceConfig(t, extra); err == nil {
				t.Fatal("accepted invalid settings")
			}
		})
	}
}

// Restored YAML knobs control the runtime DB idle pool and cache without changing defaults.
func TestRestoredPerformanceOptions(t *testing.T) {
	c, err := loadPerformanceConfig(t, "database:\n  maxIdleConns: 16\nconfigCache:\n  ttl: 5s\n  maxEntries: 8192\n  maxInflight: 24\n")
	if err != nil || c.Database.MaxIdleConns != 16 || c.ConfigCache.TTL != "5s" ||
		c.ConfigCache.MaxEntries != 8192 || c.ConfigCache.MaxInflight != 24 {
		t.Fatalf("restored settings: %+v %v", c, err)
	}
	for _, extra := range []string{
		"configCache:\n  maxEntries: 0\n",
		"configCache:\n  maxEntries: 65537\n",
		"configCache:\n  maxInflight: 257\n",
		"database:\n  maxIdleConns: -1\n",
	} {
		if _, err := loadPerformanceConfig(t, extra); err == nil {
			t.Fatalf("invalid restored parameter accepted: %s", extra)
		}
	}
}
