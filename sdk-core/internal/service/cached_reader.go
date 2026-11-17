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
	cache  *cache.Cache[publicConfigKey, publicConfigSnapshot]
}

type publicConfigSnapshot struct {
	value model.ConfigResponse
	json string
}

func NewCachedReader(reader Reader, options cache.Options) (*CachedReader, error) {
	c, err := cache.New[publicConfigKey, publicConfigSnapshot](options, func(v publicConfigSnapshot) publicConfigSnapshot {
		v.value = clonePublicConfig(v.value)
		return v
	})
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
	snapshot, err := r.lookup(ctx, appID, env)
	return snapshot.value, err
}

// LookupPublicConfigJSON returns the immutable response encoded at the same
// load as the typed value; it never adds a second TTL or caches errors.
func (r *CachedReader) LookupPublicConfigJSON(ctx context.Context, appID, env string) (string, error) {
	snapshot, err := r.lookup(ctx, appID, env)
	return snapshot.json, err
}

func (r *CachedReader) lookup(ctx context.Context, appID, env string) (publicConfigSnapshot, error) {
	return r.cache.Get(ctx, publicConfigKey{appID, env}, func(ctx context.Context) (publicConfigSnapshot, error) {
		value, err := r.reader.LookupPublicConfig(ctx, appID, env)
		if err != nil {
			return publicConfigSnapshot{}, err
		}
		encoded, err := model.EncodePublicConfig(value)
		if err != nil {
			return publicConfigSnapshot{}, err
		}
		return publicConfigSnapshot{value: value, json: encoded}, nil
	})
}

func (r *CachedReader) Close()             { r.cache.Close() }
func (r *CachedReader) Stats() cache.Stats { return r.cache.Stats() }

var _ Reader = (*CachedReader)(nil)
