package signature

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

const (
	// DefaultReplayWindow is the accepted request timestamp skew in either direction.
	DefaultReplayWindow = 5 * time.Minute
	// DefaultNonceCapacity bounds memory use for one process. Configure a shared
	// NonceStore for multi-instance/global replay protection.
	DefaultNonceCapacity = 320000
)

var (
	// ErrNonceReplay means the nonce was already accepted or the bounded store is full.
	ErrNonceReplay = errors.New("请求 nonce 已使用或防重放缓存容量已满")
	// ErrNonceStoreUnavailable means the replay store is missing or failed.
	ErrNonceStoreUnavailable = errors.New("防重放存储不可用")
)

var defaultNonceStore NonceStore = NewNonceGuard(DefaultNonceCapacity)

// DefaultNonceStore returns the process-wide bounded replay store used when a
// service does not inject a dedicated store. Applications should inject one
// shared store into all signed endpoints in the same process.
func DefaultNonceStore() NonceStore { return defaultNonceStore }

// ResolveNonceStore returns the supplied store, or the process-wide default.
func ResolveNonceStore(store NonceStore) NonceStore {
	if store != nil {
		return store
	}
	return DefaultNonceStore()
}

// NonceStore atomically accepts a nonce once within its signature validity window.
// Implementations may be process-local or distributed (for example Redis SET NX PX).
type NonceStore interface {
	Use(ctx context.Context, scope, nonce, timestamp string, now time.Time, window time.Duration) (bool, error)
}

// NonceGuard is a bounded, concurrency-safe, process-local NonceStore.
// It never evicts unexpired entries: when full it fails closed.
type NonceGuard struct {
	mu          sync.Mutex
	used        map[[32]byte]int64
	expirations map[int64][][32]byte
	lastCleanup int64
	limit       int
}

// NewNonceGuard creates a bounded in-memory replay store. A non-positive limit
// selects DefaultNonceCapacity.
func NewNonceGuard(limit int) *NonceGuard {
	if limit <= 0 {
		limit = DefaultNonceCapacity
	}
	return &NonceGuard{
		used:        make(map[[32]byte]int64),
		expirations: make(map[int64][][32]byte),
		limit:       limit,
	}
}

// Use records scope+nonce until the full timestamp acceptance window has elapsed.
// A timestamp up to one window in the future can therefore remain live for 2*window.
func (g *NonceGuard) Use(ctx context.Context, scope, nonce, timestamp string, now time.Time, window time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if g == nil {
		return false, ErrNonceStoreUnavailable
	}
	if scope == "" || nonce == "" || window <= 0 {
		return false, errors.New("防重放参数不合法")
	}
	stamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false, errors.New("防重放时间戳不合法")
	}
	signedAt := time.Unix(stamp, 0)
	expiresAt := now.Add(window)
	if signedExpiry := signedAt.Add(window); signedExpiry.After(expiresAt) {
		expiresAt = signedExpiry
	}

	digest := sha256.Sum256([]byte(scope + "\x00" + nonce))
	nowSecond := now.Unix()
	expirySecond := expiresAt.Unix()

	g.mu.Lock()
	defer g.mu.Unlock()

	if nowSecond != g.lastCleanup {
		for until, keys := range g.expirations {
			if until <= nowSecond {
				for _, key := range keys {
					if g.used[key] == until {
						delete(g.used, key)
					}
				}
				delete(g.expirations, until)
			}
		}
		g.lastCleanup = nowSecond
	}
	if _, exists := g.used[digest]; exists {
		return false, nil
	}
	if len(g.used) >= g.limit {
		return false, nil
	}
	g.used[digest] = expirySecond
	g.expirations[expirySecond] = append(g.expirations[expirySecond], digest)
	return true, nil
}

// AcceptNonce converts a duplicate/full result into ErrNonceReplay and store
// failures into ErrNonceStoreUnavailable. Call only after signature validation.
func AcceptNonce(ctx context.Context, store NonceStore, scope, nonce, timestamp string, now time.Time, window time.Duration) error {
	if store == nil {
		return ErrNonceStoreUnavailable
	}
	accepted, err := store.Use(ctx, scope, nonce, timestamp, now, window)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNonceStoreUnavailable, err)
	}
	if !accepted {
		return ErrNonceReplay
	}
	return nil
}
