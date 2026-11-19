package bootstrap

import (
	"errors"
	"time"

	"hitall.com/middle-platform/shared/cache"
)

// CacheConfig uses TTL=0 to retain uncached reads.
type CacheConfig struct {
	TTL         string `yaml:"ttl"`
	MaxEntries  int    `yaml:"maxEntries"`
	MaxInflight int    `yaml:"maxInflight"`
	LoadTimeout string `yaml:"-"`
}

func DefaultCacheConfig(ttl string) CacheConfig {
	return CacheConfig{TTL: ttl, MaxEntries: 4096, MaxInflight: 8, LoadTimeout: "5s"}
}

func (c CacheConfig) Validate() error {
	ttl, err := time.ParseDuration(c.TTL)
	if err != nil || ttl < 0 || ttl > 5*time.Second {
		return errors.New("configCache.ttl 必须在 0s 到 5s 之间")
	}
	if c.MaxEntries < 1 || c.MaxEntries > 65536 {
		return errors.New("configCache.maxEntries 必须在 1 到 65536 之间")
	}
	if c.MaxInflight < 1 || c.MaxInflight > 256 {
		return errors.New("configCache.maxInflight 必须在 1 到 256 之间")
	}
	timeout, err := time.ParseDuration(c.LoadTimeout)
	if err != nil || timeout <= 0 || timeout > 5*time.Second {
		return errors.New("configCache.loadTimeout 必须大于 0 且不超过 5s")
	}
	return nil
}

// Options must only be called after Validate.
func (c CacheConfig) Options() cache.Options {
	ttl, _ := time.ParseDuration(c.TTL)
	timeout, _ := time.ParseDuration(c.LoadTimeout)
	return cache.Options{TTL: ttl, MaxEntries: c.MaxEntries, MaxInflight: c.MaxInflight, LoadTimeout: timeout}
}
