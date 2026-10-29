package config

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

type webhookLookup func(context.Context, string) ([]net.IPAddr, error)

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

	// Local development and explicit private IP callbacks remain supported.
	if strings.EqualFold(host, "localhost") { return address.IsLoopback() }
	if literal, err := netip.ParseAddr(host); err == nil {
		return literal.Unmap() == address
	}
	return address.IsGlobalUnicast() && !address.IsPrivate() &&
		!address.IsLoopback() && !address.IsInterfaceLocalMulticast()
}

func webhookHostAllowed(host, allowed string) bool {
	for _, candidate := range strings.Split(allowed, ",") {
		if strings.EqualFold(host, strings.TrimSpace(candidate)) { return true }
	}
	return false
}

// The checked address is also the address dialed; there is no second DNS lookup.
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
