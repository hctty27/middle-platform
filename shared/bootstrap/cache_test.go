package bootstrap

import (
	"testing"
	"time"
)

func TestCacheConfig(t *testing.T) {
	for _, ttl := range []string{"0s", "1s", "5s"} {
		c := DefaultCacheConfig(ttl)
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		opts := c.Options()
		want, _ := time.ParseDuration(ttl)
		if opts.TTL != want || opts.MaxEntries != 4096 || opts.MaxInflight != 8 || opts.LoadTimeout != 5*time.Second {
			t.Fatalf("options: %+v", opts)
		}
	}
	cases := []func(*CacheConfig){
		func(c *CacheConfig) { c.TTL = "bad" }, func(c *CacheConfig) { c.TTL = "-1s" }, func(c *CacheConfig) { c.TTL = "6s" },
		func(c *CacheConfig) { c.MaxEntries = 0 }, func(c *CacheConfig) { c.MaxEntries = 65537 },
		func(c *CacheConfig) { c.MaxInflight = 0 }, func(c *CacheConfig) { c.MaxInflight = 257 },
		func(c *CacheConfig) { c.LoadTimeout = "0s" }, func(c *CacheConfig) { c.LoadTimeout = "6s" }, func(c *CacheConfig) { c.LoadTimeout = "bad" },
	}
	for i, change := range cases {
		c := DefaultCacheConfig("1s")
		change(&c)
		if c.Validate() == nil {
			t.Errorf("accepted invalid case %d", i)
		}
	}
}
