// Package settings 加载 SDK Core 的 YAML 配置和环境变量。
package settings

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"hitall.com/middle-platform/shared/bootstrap"
	"hitall.com/middle-platform/shared/logging"
)

// Database 保存 config_center 单连接池参数。
type Database struct {
	DSN             string `yaml:"dsn"`
	MaxOpenConns    int    `yaml:"maxOpenConns"`
	MaxIdleConns    int    `yaml:"-"`
	ConnMaxLifetime string `yaml:"-"`
}

// OrderNotify 保存异步发货任务的并发和空闲轮询参数。
type OrderNotify struct {
	Workers             int    `yaml:"workers"`
	IdleInterval        string `yaml:"-"`
	HostConcurrency     int    `yaml:"hostConcurrency"`
	DisableKeepAlives   bool   `yaml:"-"`
	MaxIdleConns        int    `yaml:"-"`
	MaxIdleConnsPerHost int    `yaml:"-"`
	MaxConnsPerHost     int    `yaml:"-"`
	IdleConnTimeout     string `yaml:"-"`
}

// Config 保存 SDK Core 的监听、数据库和日志配置。
type Config struct {
	Log    logging.Config `yaml:"log"`
	Server struct {
		Address        string `yaml:"address"`
		RequestTimeout string `yaml:"-"`
	} `yaml:"server"`
	Database    Database              `yaml:"database"`
	OrderNotify OrderNotify           `yaml:"orderNotify"`
	ConfigCache bootstrap.CacheConfig `yaml:"configCache"`
	AdminAuth   struct {
		Secret string `yaml:"secret"`
	} `yaml:"adminAuth"`
}

func defaultDB() Database {
	return Database{MaxOpenConns: 20, MaxIdleConns: 10, ConnMaxLifetime: "30m"}
}
func validateDB(d Database) error {
	if d.MaxOpenConns < 1 || d.MaxIdleConns < 0 || d.MaxIdleConns > d.MaxOpenConns {
		return errors.New("数据库连接池配置不合法")
	}
	if _, err := time.ParseDuration(d.ConnMaxLifetime); err != nil {
		return fmt.Errorf("数据库 connMaxLifetime 配置不合法: %w", err)
	}
	return nil
}

func validateOrderNotify(cfg OrderNotify) error {
	if cfg.Workers < 1 || cfg.Workers > 32 {
		return errors.New("orderNotify.workers 必须在 1 到 32 之间")
	}
	interval, err := time.ParseDuration(cfg.IdleInterval)
	if err != nil || interval < 100*time.Millisecond || interval > 10*time.Second {
		return errors.New("orderNotify.idleInterval 必须在 100ms 到 10s 之间")
	}
	return nil
}

// Load 严格解析单份 YAML，并允许环境变量覆盖连接和监听地址。
func Load(path string) (Config, error) {
	cfg := Config{}
	cfg.Log = logging.Default()
	cfg.Server.Address = ":8082"
	cfg.Database = defaultDB()
	cfg.OrderNotify = OrderNotify{Workers: 4, IdleInterval: "500ms", HostConcurrency: 1,
		MaxIdleConns: 32, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4, IdleConnTimeout: "30s"}
	cfg.ConfigCache = bootstrap.DefaultCacheConfig("1s")
	cfg.Server.RequestTimeout = "5s"
	if path == "" {
		path = "configs/config.yaml"
	}
	if err := bootstrap.ReadYAML(path, &cfg); err != nil {
		return Config{}, err
	}
	if v, ok := os.LookupEnv("SDK_CORE_DB_DSN"); ok {
		cfg.Database.DSN = v
	}
	if v, ok := os.LookupEnv("SDK_CORE_ADDR"); ok {
		cfg.Server.Address = v
	}
	if v, ok := os.LookupEnv("ADMIN_AUTH_SECRET"); ok {
		cfg.AdminAuth.Secret = v
	}
	if err := cfg.Log.ApplyEnv(); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(cfg.Database.DSN) == "" {
		return Config{}, errors.New("必须配置 database.dsn 或 SDK_CORE_DB_DSN")
	}
	if strings.TrimSpace(cfg.Server.Address) == "" {
		return Config{}, errors.New("必须配置 server.address")
	}
	if err := validateDB(cfg.Database); err != nil {
		return Config{}, err
	}
	if err := validateOrderNotify(cfg.OrderNotify); err != nil {
		return Config{}, err
	}
	if err := validatePerformance(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
