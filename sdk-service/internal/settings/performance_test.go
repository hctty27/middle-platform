package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func loadPerformanceConfig(t *testing.T, extra string) (Config, error) {
	t.Helper()
	t.Setenv("SDK_SERVICE_DB_DSN", "test-dsn")
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("database:\n  dsn: test-dsn\n"+extra), 0600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}
func TestPerformanceDefaults(t *testing.T) {
	c, err := loadPerformanceConfig(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ConfigCache.TTL != "0s" || c.Server.RequestTimeout != "8s" {
		t.Fatalf("defaults: %+v", c)
	}
	c, err = loadPerformanceConfig(t, "game:\n  timeout: 10s\nconfigCache:\n  ttl: 1s\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.ConfigCache.TTL != "1s" || c.Server.RequestTimeout != "13s" {
		t.Fatalf("custom: %+v", c)
	}
	c, err = loadPerformanceConfig(t, "server:\n  requestTimeout: 2s\n")
	if err != nil || c.Server.RequestTimeout != "2s" {
		t.Fatalf("explicit timeout: %+v %v", c, err)
	}
}
func TestAutoBudgetPreservesPlatformTimeout(t *testing.T) {
	for _, gameTimeout := range []string{"1ms", "1s", "5s"} {
		c, err := loadPerformanceConfig(t, "game:\n  timeout: "+gameTimeout+"\n")
		if err != nil {
			t.Fatal(err)
		}
		if c.Server.RequestTimeout != "8s" {
			t.Fatalf("game timeout %s reduced platform budget: %s", gameTimeout, c.Server.RequestTimeout)
		}
	}
}
func TestExamplePerformanceConfiguration(t *testing.T) {
	t.Setenv("SDK_SERVICE_DB_DSN", "test-dsn")
	c, err := Load(filepath.Join("..", "..", "configs", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ConfigCache.TTL != "0s" || c.Server.RequestTimeout != "8s" {
		t.Fatalf("example drift: %+v", c)
	}
}
func TestPerformanceInvalidConfigurations(t *testing.T) {
	for _, extra := range []string{
		"configCache:\n  ttl: -1s\n", "configCache:\n  maxEntries: 0\n", "configCache:\n  loadTimeout: 0s\n",
		"server:\n  requestTimeout: bad\n", "server:\n  requestTimeout: 36s\n", "server:\n  requestTimeout: 0s\n",
	} {
		t.Run(extra, func(t *testing.T) {
			if _, err := loadPerformanceConfig(t, extra); err == nil {
				t.Fatal("accepted invalid settings")
			}
		})
	}
}
