package service

import (
	"context"
	"encoding/json"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/shared/cache"
)

// OrderConfigRepository is the single repository used by all three order reads.
// Unlike Reader (public configuration), these reads must retain signing secrets.
type OrderConfigRepository interface {
	OrderConfigReader
	LookupPlatformSecret(context.Context, string) (string, error)
	LookupGameConfig(context.Context, int64) (json.RawMessage, error)
}

type orderConfigKind uint8

const (
	orderConfigLookup orderConfigKind = iota
	orderConfigPlatformSecret
	orderConfigGame
)

type orderConfigKey struct {
	kind       orderConfigKind
	appID, env string
	gameID     int64
}

type orderConfigSnapshot struct {
	app    model.ChannelApp
	group  *model.BoundGroup
	secret string
	game   json.RawMessage
}

// CachedOrderReader caches successful order configuration reads, including
// secrets, in one bounded cache. MaxEntries and MaxInflight apply across all
// three read methods, not separately to each method. Keys and values must never
// be logged; Stats exposes only the shared cache's fixed-size counters.
//
// Pass validated cfg.ConfigCache.Options() (deployment default TTL: 1s). TTL=0
// disables caching, miss coalescing and load limiting; it is NOT a default TTL.
// Disabled reads use the caller context directly and still copy mutable results.
// Enabled reads honor each caller's cancellation independently; a shared load
// continues under LoadTimeout even when its initiating caller stops waiting.
//
// There is no push invalidation. Application/binding disablement and secret
// rotation can remain stale for up to TTL plus an in-flight repository read's
// delay, because TTL starts when that read completes (bounded by LoadTimeout
// for successful shared loads). Existing request snapshots may live longer.
// Use TTL=0 for direct reads, or Close and replace this reader to discard its
// entries and cancel shared loads; Close permanently rejects subsequent reads.
// This is not a dual-key rotation/grace-period mechanism.
type CachedOrderReader struct {
	repository OrderConfigRepository
	cache      *cache.Cache[orderConfigKey, orderConfigSnapshot]
	disabled   bool
}

func NewCachedOrderReader(repository OrderConfigRepository, options cache.Options) (*CachedOrderReader, error) {
	c, err := cache.New[orderConfigKey, orderConfigSnapshot](options, cloneOrderConfig)
	if err != nil {
		return nil, err
	}
	return &CachedOrderReader{repository: repository, cache: c, disabled: options.TTL == 0}, nil
}

func cloneOrderConfig(v orderConfigSnapshot) orderConfigSnapshot {
	v.app.Config = cloneOrderJSON(v.app.Config)
	v.app.GameConfig = cloneOrderJSON(v.app.GameConfig)
	v.app.GameSDKConfig = cloneOrderJSON(v.app.GameSDKConfig)
	if v.group != nil {
		group := *v.group
		group.RegionConfig = cloneOrderJSON(group.RegionConfig)
		group.RegionSDKConfig = cloneOrderJSON(group.RegionSDKConfig)
		group.Config = cloneOrderJSON(group.Config)
		group.SDKConfig = cloneOrderJSON(group.SDKConfig)
		v.group = &group
	}
	v.game = cloneOrderJSON(v.game)
	return v
}

func cloneOrderJSON(v json.RawMessage) json.RawMessage {
	if v == nil {
		return nil
	}
	return append(make(json.RawMessage, 0, len(v)), v...)
}

func (r *CachedOrderReader) get(ctx context.Context, key orderConfigKey, load func(context.Context) (orderConfigSnapshot, error)) (orderConfigSnapshot, error) {
	value, err := r.cache.Get(ctx, key, load)
	// The shared cache deliberately bypasses cloning along with caching at TTL=0.
	if r.disabled && err == nil {
		value = cloneOrderConfig(value)
	}
	return value, err
}

func (r *CachedOrderReader) Lookup(ctx context.Context, appID, env string) (model.ChannelApp, *model.BoundGroup, error) {
	value, err := r.get(ctx, orderConfigKey{kind: orderConfigLookup, appID: appID, env: env}, func(ctx context.Context) (orderConfigSnapshot, error) {
		app, group, err := r.repository.Lookup(ctx, appID, env)
		return orderConfigSnapshot{app: app, group: group}, err
	})
	return value.app, value.group, err
}

func (r *CachedOrderReader) LookupPlatformSecret(ctx context.Context, appID string) (string, error) {
	value, err := r.get(ctx, orderConfigKey{kind: orderConfigPlatformSecret, appID: appID}, func(ctx context.Context) (orderConfigSnapshot, error) {
		secret, err := r.repository.LookupPlatformSecret(ctx, appID)
		return orderConfigSnapshot{secret: secret}, err
	})
	return value.secret, err
}

func (r *CachedOrderReader) LookupGameConfig(ctx context.Context, gameID int64) (json.RawMessage, error) {
	value, err := r.get(ctx, orderConfigKey{kind: orderConfigGame, gameID: gameID}, func(ctx context.Context) (orderConfigSnapshot, error) {
		game, err := r.repository.LookupGameConfig(ctx, gameID)
		return orderConfigSnapshot{game: game}, err
	})
	return value.game, err
}

func (r *CachedOrderReader) Close()             { r.cache.Close() }
func (r *CachedOrderReader) Stats() cache.Stats { return r.cache.Stats() }

var _ OrderConfigRepository = (*CachedOrderReader)(nil)
