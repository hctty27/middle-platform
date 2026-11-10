package settings

import (
	"errors"
	"strings"

	"hitall.com/middle-platform/shared/bootstrap"
	"hitall.com/middle-platform/shared/logging"
)

// Settings 保存配置中心运行参数。
type Settings struct {
	DSN              string
	Address          string
	Log              logging.Config
	AdminSecret      string
	ConfigSyncSecret string
}

type configFile struct {
	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`
	Database struct {
		DSN string `yaml:"dsn"`
	} `yaml:"database"`
	Log       logging.Config `yaml:"log"`
	AdminAuth struct {
		Secret string `yaml:"secret"`
	} `yaml:"adminAuth"`
	ConfigSync struct {
		Secret string `yaml:"secret"`
	} `yaml:"configSync"`
}

// Load 使用默认 YAML 文件启动。
func Load() (Settings, error) { return LoadFile("") }

// LoadFile 只读取指定 YAML（空路径使用 configs/config.yaml），再应用环境变量覆盖。
func LoadFile(path string) (Settings, error) {
	data := configFile{Log: logging.Default()}
	if path == "" {
		path = "configs/config.yaml"
	}
	if err := bootstrap.ReadYAML(path, &data); err != nil {
		return Settings{}, err
	}

	cfg := Settings{Address: "127.0.0.1:8080", DSN: data.Database.DSN, Log: data.Log, AdminSecret: data.AdminAuth.Secret, ConfigSyncSecret: data.ConfigSync.Secret}
	if data.Server.Address != "" {
		cfg.Address = data.Server.Address
	}
	// 环境变量优先级最高；管理与配置同步 API 内建 HMAC-SHA256 验签。
	bootstrap.OverrideString("CONFIG_DB_DSN", &cfg.DSN)
	bootstrap.OverrideString("CONFIG_GO_ADDR", &cfg.Address)
	bootstrap.OverrideString("ADMIN_AUTH_SECRET", &cfg.AdminSecret)
	bootstrap.OverrideString("CONFIG_SYNC_SECRET", &cfg.ConfigSyncSecret)
	if err := cfg.Log.ApplyEnv(); err != nil {
		return Settings{}, err
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		return Settings{}, errors.New("必须配置 database.dsn 或 CONFIG_DB_DSN")
	}
	if len([]byte(cfg.ConfigSyncSecret)) < 32 || strings.TrimSpace(cfg.ConfigSyncSecret) != cfg.ConfigSyncSecret {
		return Settings{}, errors.New("必须配置至少 32 字节的 configSync.secret 或 CONFIG_SYNC_SECRET")
	}
	return cfg, nil
}
