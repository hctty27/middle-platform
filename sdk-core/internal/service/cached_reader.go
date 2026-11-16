package service

import (
	"context"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/shared/cache"
)

type publicConfigKey struct{ appID, env string }

// CachedReader caches only public configuration. It deliberately does not
// implement the order, platform-secret or game-configuration readers.
type CachedReader struct {
	reader Reader
	cache  *cache.Cache[publicConfigKey, model.ConfigResponse]
}

func NewCachedReader(reader Reader, options cache.Options) (*CachedReader, error) {
	c, err := cache.New[publicConfigKey, model.ConfigResponse](options, clonePublicConfig)
	if err != nil {
		return nil, err
	}
	return &CachedReader{reader: reader, cache: c}, nil
}

func clonePublicConfig(v model.ConfigResponse) model.ConfigResponse {
	if v.TsKey != nil {
		key := *v.TsKey
		v.TsKey = &key
	}
	return v
}

func (r *CachedReader) LookupPublicConfig(ctx context.Context, appID, env string) (model.ConfigResponse, error) {
	return r.cache.Get(ctx, publicConfigKey{appID, env}, func(ctx context.Context) (model.ConfigResponse, error) {
		return r.reader.LookupPublicConfig(ctx, appID, env)
	})
}

func (r *CachedReader) Close()             { r.cache.Close() }
func (r *CachedReader) Stats() cache.Stats { return r.cache.Stats() }

var _ Reader = (*CachedReader)(nil)
