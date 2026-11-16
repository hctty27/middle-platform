package settings

import (
	"errors"
	"time"
)

func validatePerformance(cfg Config) error {
	if err := cfg.ConfigCache.Validate(); err != nil {
		return err
	}
	if d, err := time.ParseDuration(cfg.Server.RequestTimeout); err != nil || d <= 0 || d > 35*time.Second {
		return errors.New("server.requestTimeout 必须大于 0 且不超过 35s，或使用 auto")
	}
	return nil
}
