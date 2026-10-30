// Package webhook 实现安全回调、签名、重试和集群租约。
package webhook

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"github.com/hctty27/middle-platform/config-center/internal/model"
)

var ErrInvalid = model.ErrInvalid

// webhookLookup 可注入域名解析器，用于测试真实连接目标的安全校验。
type webhookLookup func(context.Context, string) ([]net.IPAddr, error)

// forbiddenWebhookRanges 保存回调目标禁止使用的特殊网络地址段。
var forbiddenWebhookRanges = func() []netip.Prefix {
	blocks := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24",
		"192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32",
	}
	ranges := make([]netip.Prefix, 0, len(blocks))
	for _, block := range blocks {
		ranges = append(ranges, netip.MustParsePrefix(block))
	}
	return ranges
}()

// permittedWebhookIP 判断目标 IP 是否符合安全规则。
func permittedWebhookIP(host string, address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() ||
		address.IsMulticast() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() {
		return false
	}
	for _, block := range forbiddenWebhookRanges {
		if block.Contains(address) { return false }
	}

	// 显式配置的本地回调地址仍可使用环回 IP，但必须经过主机白名单检查。
	if strings.EqualFold(host, "localhost") { return address.IsLoopback() }
	if literal, err := netip.ParseAddr(host); err == nil {
		return literal.Unmap() == address
	}
	return address.IsGlobalUnicast() && !address.IsPrivate() &&
		!address.IsLoopback() && !address.IsInterfaceLocalMulticast()
}

// webhookHostAllowed 以精确匹配方式检查回调主机白名单。
func webhookHostAllowed(host, allowed string) bool {
	for _, candidate := range strings.Split(allowed, ",") {
		if strings.EqualFold(host, strings.TrimSpace(candidate)) { return true }
	}
	return false
}

// 校验与实际建连使用同一解析结果，避免第二次 DNS 解析引入重绑定攻击。
// secureWebhookDial 校验解析 IP 并直接连接该 IP，避免 DNS 重绑定。
func secureWebhookDial(allowedHosts string, lookup webhookLookup) func(context.Context, string, string) (net.Conn, error) {
	if lookup == nil { lookup = net.DefaultResolver.LookupIPAddr }
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil { return nil, err }
		if !webhookHostAllowed(host, allowedHosts) {
			return nil, fmt.Errorf("webhook host %q is not allowed", host)
		}
		ips, err := lookup(ctx, host)
		if err != nil { return nil, err }
		if len(ips) == 0 { return nil, fmt.Errorf("webhook host %q has no DNS records", host) }
		for _, item := range ips {
			ip, ok := netip.AddrFromSlice(item.IP)
			if !ok || !permittedWebhookIP(host, ip) {
				return nil, fmt.Errorf("webhook host %q resolves to a prohibited address", host)
			}
		}
		var dialErr error
		for _, item := range ips {
			destination := net.JoinHostPort(item.IP.String(), port)
			var conn net.Conn
			conn, dialErr = dialer.DialContext(ctx, network, destination)
			if dialErr == nil { return conn, nil }
		}
		return nil, fmt.Errorf("webhook connection failed: %w", dialErr)
	}
}

// ValidateCallback 校验回调地址、协议以及白名单。
func ValidateCallback(raw, allowed string) error {
	if len(raw) > 1024 || strings.TrimSpace(raw) == "" { return ErrInvalid }
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" { return ErrInvalid }
	host := strings.ToLower(u.Hostname())
	if !webhookHostAllowed(host, allowed) { return ErrInvalid }
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())) {
		return ErrInvalid
	}
	return nil
}
