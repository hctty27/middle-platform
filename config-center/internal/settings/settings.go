// Package settings 加载配置中心运行参数。
package settings

import (
	"errors"
	"os"
	"strings"
)

// Settings 描述数据库、监听端口以及读写和回调开关。
type Settings struct {
    DSN string
    Address string
    AllowedHosts string
    WritesEnabled bool
    WebhookEnabled bool
    AllowLegacyUpgrade bool
}
// Load 读取和校验环境变量，保持原有变量及默认端口兼容。
func Load() (Settings, error) {
    cfg := Settings{
        DSN: os.Getenv("CONFIG_DB_DSN"),
        Address: os.Getenv("CONFIG_GO_ADDR"),
        AllowedHosts: os.Getenv("WEBHOOK_ALLOWED_HOSTS"),
        // 管理写接口和 Webhook 由显式开关控制，避免只读节点误启写入。
        WritesEnabled: strings.EqualFold(os.Getenv("CONFIG_GO_ENABLE_WRITES"), "true"),
        WebhookEnabled: strings.EqualFold(os.Getenv("CONFIG_GO_ENABLE_WEBHOOK"), "true"),
        AllowLegacyUpgrade: strings.EqualFold(os.Getenv("CONFIG_ALLOW_LEGACY_UPGRADE"), "true"),
    }
    if cfg.DSN == "" { return Settings{}, errors.New("CONFIG_DB_DSN is required") }
    if cfg.Address == "" { cfg.Address = ":8081" }
    // 发送回调需要完整的写入与变更记录能力，不允许仅启用回调。
    if cfg.WebhookEnabled && !cfg.WritesEnabled {
        return Settings{}, errors.New("webhook requires CONFIG_GO_ENABLE_WRITES=true")
    }
    return cfg, nil
}
