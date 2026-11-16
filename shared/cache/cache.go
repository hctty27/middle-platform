// Package cache provides bounded, process-local successful-value caching.
package cache

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

var ErrClosed = errors.New("cache: closed")

// Options configures the cache. TTL zero bypasses caching and load limiting.
// Zero limits and timeout select defaults when caching is enabled. Negative
// values are invalid. Callers should enforce deployment-specific upper limits.
type Options struct {
	TTL         time.Duration
	MaxEntries  int
	MaxInflight int
	LoadTimeout time.Duration
}

// Stats is a fixed-size snapshot; it never contains keys or cached values.
// Hits counts immediate cache hits; Misses counts requests missing on their first
// lookup. Waited counts requests that waited for capacity, once per Get even if
// woken repeatedly. Shared counts requests joining a loader; Loads counts actual
// loaders. LoadErrors includes timeouts and shutdown cancellation. Gauges reflect
// current entries and actual loaders (including loaders still returning after
// Close). Disabled caches do not collect statistics.
type Stats struct {
	Hits, Misses, Loads, Shared, Waited, LoadErrors, Evictions uint64
	Entries, Inflight                                          int
}

type entry[K comparable, V any] struct {
	key     K
	value   V
	expires time.Time
}
type flight[V any] struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	value  V
	err    error
}

// Cache requires clone to isolate all mutable parts of V. Nil means V is safe
// to copy by value. Loaders must honor context cancellation; an uncooperative
// loader retains its slot until it returns, rather than allowing unbounded work.
type Cache[K comparable, V any] struct {
	mu      sync.Mutex
	options Options
	clone   func(V) V
	now     func() time.Time
	closed  bool
	entries map[K]*list.Element
	lru     list.List
	flights map[K]*flight[V]
	changed chan struct{} // Closed on capacity release or shutdown, under mu.
	stats   Stats
}

func New[K comparable, V any](options Options, clone func(V) V) (*Cache[K, V], error) {
	if options.TTL < 0 || options.MaxEntries < 0 || options.MaxInflight < 0 || options.LoadTimeout < 0 {
		return nil, errors.New("cache: options must not be negative")
	}
	if options.MaxEntries == 0 {
		options.MaxEntries = 4096
	}
	if options.MaxInflight == 0 {
		options.MaxInflight = 8
	}
	if options.LoadTimeout == 0 {
		options.LoadTimeout = 5 * time.Second
	}
	if clone == nil {
		clone = func(v V) V { return v }
	}
	c := &Cache[K, V]{options: options, clone: clone, now: time.Now}
	if options.TTL != 0 {
		c.entries = make(map[K]*list.Element)
		c.flights = make(map[K]*flight[V])
		c.changed = make(chan struct{})
	}
	return c, nil
}

// Get waits for load capacity in the calling goroutine, bounded by ctx and
// Close. It neither bypasses MaxInflight nor creates a goroutine for a queued
// request. Capacity waiters recheck the cache and same-key loads after waking;
// there is no FIFO guarantee between different keys.
func (c *Cache[K, V]) Get(ctx context.Context, key K, load func(context.Context) (V, error)) (V, error) {
	var zero V
	missed, waited := false, false
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return zero, ErrClosed
		}
		if c.options.TTL == 0 {
			c.mu.Unlock()
			return load(ctx)
		}
		if element, ok := c.entries[key]; ok {
			e := element.Value.(*entry[K, V])
			if c.now().Before(e.expires) {
				c.lru.MoveToFront(element)
				if !missed {
					c.stats.Hits++
				}
				value := e.value
				c.mu.Unlock()
				return c.clone(value), nil
			}
			delete(c.entries, key)
			c.lru.Remove(element)
		}
		if !missed {
			c.stats.Misses++
			missed = true
		}
		if f, ok := c.flights[key]; ok {
			c.stats.Shared++
			c.mu.Unlock()
			return c.wait(ctx, f)
		}
		if len(c.flights) >= c.options.MaxInflight {
			if !waited {
				c.stats.Waited++
				waited = true
			}
			changed := c.changed
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-changed:
				continue
			}
		}
		loadCtx, cancel := context.WithTimeout(context.Background(), c.options.LoadTimeout)
		f := &flight[V]{ctx: loadCtx, cancel: cancel, done: make(chan struct{})}
		c.flights[key] = f
		c.stats.Loads++
		c.mu.Unlock()
		go c.run(key, f, load)
		return c.wait(ctx, f)
	}
}

func (c *Cache[K, V]) wait(ctx context.Context, f *flight[V]) (V, error) {
	var zero V
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-f.done:
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if f.err != nil {
			return zero, f.err
		}
		return c.clone(f.value), nil
	case <-f.ctx.Done():
		// Completion cancels the timer as well. Prefer a published result over
		// that cleanup cancellation, but never over the waiter's own cancellation.
		select {
		case <-f.done:
			if err := ctx.Err(); err != nil {
				return zero, err
			}
			if f.err != nil {
				return zero, f.err
			}
			return c.clone(f.value), nil
		default:
			return zero, f.ctx.Err()
		}
	}
}

func (c *Cache[K, V]) run(key K, f *flight[V], load func(context.Context) (V, error)) {
	value, err := load(f.ctx)
	// Capture a private value once, before publishing to any waiter or the LRU.
	if err == nil {
		value = c.clone(value)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctxErr := f.ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	if c.closed && err == nil {
		err = ErrClosed
	}
	if err == nil {
		if len(c.entries) >= c.options.MaxEntries {
			oldest := c.lru.Back()
			delete(c.entries, oldest.Value.(*entry[K, V]).key)
			c.lru.Remove(oldest)
			c.stats.Evictions++
		}
		c.entries[key] = c.lru.PushFront(&entry[K, V]{key: key, value: value, expires: c.now().Add(c.options.TTL)})
	} else {
		c.stats.LoadErrors++
	}
	f.value, f.err = value, err
	delete(c.flights, key)
	if !c.closed {
		close(c.changed)
		c.changed = make(chan struct{})
	}
	close(f.done)
	// Cancel only after publication; waiters prefer the completed result.
	// The timeout context is otherwise released here, not retained for the TTL.
	f.cancel()
}

// Close rejects new gets, wakes capacity waiters, clears cached values and
// cancels shared loads. It does not wait for loaders which ignore cancellation.
// TTL-zero calls already delegated to their caller context are not shared loads
// and are not canceled.
func (c *Cache[K, V]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if c.changed != nil {
		close(c.changed)
		c.changed = nil
	}
	clear(c.entries)
	c.lru.Init()
	for _, f := range c.flights {
		f.cancel()
	}
}

func (c *Cache[K, V]) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	s.Entries, s.Inflight = len(c.entries), len(c.flights)
	return s
}
