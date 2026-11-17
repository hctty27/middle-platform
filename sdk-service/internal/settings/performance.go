package settings

import (
	"errors"
	"time"
)

func validatePerformance(cfg Config) error {
	if cfg.Platform.MaxIdlePerHost < 1 || cfg.Platform.MaxIdlePerHost > 256 ||
		cfg.Platform.EventMaxConns < 0 || cfg.Platform.EventMaxConns > 4096 ||
		(!cfg.Platform.IsolateConnections && cfg.Platform.EventMaxConns != 0) {
		return errors.New("platform 连接池配置不合法")
	}
	if err := cfg.ConfigCache.Validate(); err != nil {
		return err
	}
	if d, err := time.ParseDuration(cfg.Server.RequestTimeout); err != nil || d <= 0 || d > 35*time.Second {
		return errors.New("server.requestTimeout 必须大于 0 且不超过 35s，或使用 auto")
	}
	return nil
}
