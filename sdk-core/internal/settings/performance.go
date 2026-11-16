package settings

import (
	"errors"
	"time"
)

func validatePerformance(cfg Config) error {
	if err := cfg.ConfigCache.Validate(); err != nil {
		return err
	}
	if d, err := time.ParseDuration(cfg.Server.RequestTimeout); err != nil || d <= 0 || d > 10*time.Second {
		return errors.New("server.requestTimeout 必须大于 0 且不超过 10s")
	}
	n := cfg.OrderNotify
	if n.HostConcurrency < 0 || n.HostConcurrency > 32 {
		return errors.New("orderNotify.hostConcurrency 必须在 0 到 32 之间")
	}
	if n.Workers > 1 && n.HostConcurrency >= n.Workers {
		return errors.New("orderNotify.hostConcurrency 必须小于 workers，或设为 0 关闭")
	}
	if n.MaxIdleConns < 1 || n.MaxIdleConns > 4096 || n.MaxIdleConnsPerHost < 1 || n.MaxIdleConnsPerHost > n.MaxIdleConns {
		return errors.New("orderNotify 空闲连接池配置不合法")
	}
	if n.MaxConnsPerHost < 1 || n.MaxConnsPerHost > 1024 || n.MaxConnsPerHost < n.HostConcurrency {
		return errors.New("orderNotify.maxConnsPerHost 必须在 1 到 1024 之间且不小于 hostConcurrency")
	}
	if d, err := time.ParseDuration(n.IdleConnTimeout); err != nil || d <= 0 || d > 5*time.Minute {
		return errors.New("orderNotify.idleConnTimeout 必须大于 0 且不超过 5m")
	}
	return nil
}
