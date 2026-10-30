// Package settings 加载 SDK Core 的 YAML 配置和环境变量。
package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"github.com/hctty27/middle-platform/shared/logging"
)

type Config struct {
	Log logging.Config `yaml:"log"`
	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`
	Database struct {
		DSN             string `yaml:"dsn"`
		MaxOpenConns    int    `yaml:"maxOpenConns"`
		MaxIdleConns    int    `yaml:"maxIdleConns"`
		ConnMaxLifetime string `yaml:"connMaxLifetime"`
	} `yaml:"database"`
}

func Load(path string) (Config, error) {
	cfg := Config{}
	cfg.Log = logging.Default()
	cfg.Server.Address = ":8082"
	cfg.Database.MaxOpenConns = 20
	cfg.Database.MaxIdleConns = 10
	cfg.Database.ConnMaxLifetime = "30m"

	if path == "" {
		path = "configs/config.yaml"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("configuration must contain exactly one YAML document")
	}
	if value, ok := os.LookupEnv("SDK_CORE_DB_DSN"); ok {
		cfg.Database.DSN = value
	}
	if value, ok := os.LookupEnv("SDK_CORE_ADDR"); ok {
		cfg.Server.Address = value
	}
	if err := cfg.Log.ApplyEnv(); err != nil { return Config{}, err }
	if strings.TrimSpace(cfg.Database.DSN) == "" {
		return Config{}, errors.New("database.dsn or SDK_CORE_DB_DSN is required")
	}
	if strings.TrimSpace(cfg.Server.Address) == "" {
		return Config{}, errors.New("server.address is required")
	}
	if cfg.Database.MaxOpenConns < 1 || cfg.Database.MaxIdleConns < 0 ||
		cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
		return Config{}, errors.New("invalid database pool configuration")
	}
	if _, err := time.ParseDuration(cfg.Database.ConnMaxLifetime); err != nil {
		return Config{}, fmt.Errorf("invalid database.connMaxLifetime: %w", err)
	}
	return cfg, nil
}
