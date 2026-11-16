package store

import (
	"context"

	"hitall.com/middle-platform/sdk-service/internal/model"
	"hitall.com/middle-platform/shared/cache"
)

// RouteReader preserves the distinct user-check and other-service queries.
// VerifySchema intentionally remains on the underlying Store, before wrapping.
type RouteReader interface {
	Lookup(context.Context, string, string) (model.Target, error)
	LookupService(context.Context, string, string, string) (model.Target, error)
}

type routeCacheKey struct{ namespace, appID, env, service string }

// CachedReader shares a single bounded capacity across all route namespaces.
// Target contains only value fields; no secret-bearing keys or values enter stats.
type CachedReader struct {
	reader RouteReader
	cache  *cache.Cache[routeCacheKey, model.Target]
}

func NewCachedReader(reader RouteReader, options cache.Options) (*CachedReader, error) {
	c, err := cache.New[routeCacheKey, model.Target](options, nil)
	if err != nil {
		return nil, err
	}
	return &CachedReader{reader: reader, cache: c}, nil
}

func (r *CachedReader) Lookup(ctx context.Context, appID, env string) (model.Target, error) {
	return r.cache.Get(ctx, routeCacheKey{"user_check", appID, env, "user_check"}, func(ctx context.Context) (model.Target, error) {
		return r.reader.Lookup(ctx, appID, env)
	})
}

func (r *CachedReader) LookupService(ctx context.Context, appID, env, service string) (model.Target, error) {
	return r.cache.Get(ctx, routeCacheKey{"service", appID, env, service}, func(ctx context.Context) (model.Target, error) {
		return r.reader.LookupService(ctx, appID, env, service)
	})
}

func (r *CachedReader) Close()             { r.cache.Close() }
func (r *CachedReader) Stats() cache.Stats { return r.cache.Stats() }

var _ RouteReader = (*CachedReader)(nil)
