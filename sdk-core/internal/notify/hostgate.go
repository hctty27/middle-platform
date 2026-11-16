package notify

import (
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/idna"
)

// HostGate 是每进程、每目标的非阻塞在途配额。只保留活跃目标，
// 因此条目数不超过调用者并发数；不合并 DNS 别名。worker=1 时无跨目标隔离收益。
type HostGate struct {
	mu     sync.Mutex
	limit  int
	active map[string]int
}

// NewHostGate 创建目标配额，limit<=0 关闭保护。
func NewHostGate(limit int) *HostGate { return &HostGate{limit: limit} }

// TryAcquire 按规范化 hostname+effective port 获取额度，不按 AppID/path 拆分。
// 非法目标留给 Notifier.ValidateTarget 报错，不将其无限延期。
// 成功后调用者必须释放；release 可安全重复调用。
func (g *HostGate) TryAcquire(raw string) (release func(), ok bool) {
	noop := func() {}
	if g == nil || g.limit <= 0 {
		return noop, true
	}
	key, valid := targetHostKey(raw)
	if !valid {
		return noop, true
	}
	g.mu.Lock()
	if g.active[key] >= g.limit {
		g.mu.Unlock()
		return nil, false
	}
	if g.active == nil {
		g.active = make(map[string]int)
	}
	g.active[key]++
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.active[key] <= 1 {
				delete(g.active, key)
			} else {
				g.active[key]--
			}
		})
	}, true
}

func targetHostKey(raw string) (string, bool) {
	if err := validateTarget(raw); err != nil {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := u.Hostname()
	if strings.ContainsFunc(host, func(r rune) bool { return r >= 128 }) {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil {
			return "", false
		}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.Unmap().String()
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else {
		n, _ := strconv.Atoi(port)
		port = strconv.Itoa(n)
	}
	return net.JoinHostPort(host, port), true
}
