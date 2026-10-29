// Package settings 加载并校验接入服务的环境变量。
package settings

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 包含 HTTP、数据库、配置中心和补偿任务的运行参数。
type Config struct {
	Address           string
	DSN               string
	CenterURL         string
	WebhookSecret     string
	ConnectTimeout    time.Duration
	RequestTimeout    time.Duration
	ReconcileInterval time.Duration
}

// Load 在启动时拒绝危险或不完整的配置。
func Load() (Config, error) {
	cfg := Config{
		Address:       value("CONFIG_ACCESS_ADDR", ":8081"),
		DSN:           os.Getenv("ACCESS_DB_DSN"),
		CenterURL:     strings.TrimRight(value("CONFIG_CENTER_URL", "http://127.0.0.1:8080"), "/"),
		WebhookSecret: os.Getenv("CONFIG_WEBHOOK_SECRET"),
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return cfg, fmt.Errorf("ACCESS_DB_DSN is required")
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" || cfg.WebhookSecret == "change-me-in-development" {
		return cfg, fmt.Errorf("CONFIG_WEBHOOK_SECRET must be configured")
	}
	u, err := url.Parse(cfg.CenterURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return cfg, fmt.Errorf("CONFIG_CENTER_URL must be an http(s) origin without path")
	}
	cfg.ConnectTimeout, err = millis("CONFIG_CLIENT_CONNECT_TIMEOUT_MS", 2000)
	if err != nil {
		return cfg, err
	}
	cfg.RequestTimeout, err = millis("CONFIG_CLIENT_READ_TIMEOUT_MS", 15000)
	if err != nil {
		return cfg, err
	}
	cfg.ReconcileInterval = 5 * time.Minute
	if raw := os.Getenv("CONFIG_RECONCILE_INTERVAL"); raw != "" {
		cfg.ReconcileInterval, err = time.ParseDuration(raw)
		if err != nil || cfg.ReconcileInterval < time.Second {
			return cfg, fmt.Errorf("CONFIG_RECONCILE_INTERVAL must be a duration >= 1s")
		}
	}
	return cfg, nil
}

func value(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func millis(name string, fallback int) (time.Duration, error) {
	raw := value(name, strconv.Itoa(fallback))
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 1 || ms > 120000 {
		return 0, fmt.Errorf("%s must be 1..120000 milliseconds", name)
	}
	return time.Duration(ms) * time.Millisecond, nil
}
