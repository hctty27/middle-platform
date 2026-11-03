// Package webhook 实现安全回调、签名、重试和集群租约。
package webhook

import (
    "context"
    "fmt"
    "net"
    "net/netip"
    "net/url"
    "strconv"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/config-center/internal/model"
)

var ErrInvalid = model.ErrInvalid

type webhookLookup func(context.Context, string) ([]net.IPAddr, error)

var blockedCallbackNetworks = []netip.Prefix{
    netip.MustParsePrefix("0.0.0.0/8"),
    netip.MustParsePrefix("240.0.0.0/4"),
    netip.MustParsePrefix("100.100.100.200/32"),
    netip.MustParsePrefix("fd00:ec2::254/128"),
}

// permittedWebhookIP 允许公网、内网与环回地址，但拒绝元数据及特殊网络目标。
func permittedWebhookIP(ip netip.Addr) bool {
    ip = ip.Unmap()
    if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() ||
        ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
        return false
    }
    for _, block := range blockedCallbackNetworks {
        if block.Contains(ip) {
            return false
        }
    }
    return true
}

func prohibitedMetadataHost(host string) bool {
    switch strings.ToLower(strings.TrimSuffix(host, ".")) {
    case "metadata.google.internal", "metadata.amazonaws.com", "metadata.tencentyun.com", "metadata.aliyun.com":
        return true
    }
    return false
}

// secureWebhookDial 校验实际解析到的 IP 并直连，避免 DNS 重绑定绕过。
func secureWebhookDial(lookup webhookLookup) func(context.Context, string, string) (net.Conn, error) {
    if lookup == nil {
        lookup = net.DefaultResolver.LookupIPAddr
    }
    dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
    return func(ctx context.Context, network, address string) (net.Conn, error) {
        host, port, err := net.SplitHostPort(address)
        if err != nil {
            return nil, err
        }
        if prohibitedMetadataHost(host) {
            return nil, fmt.Errorf("webhook metadata endpoint is prohibited")
        }
        ips, err := lookup(ctx, host)
        if err != nil {
            return nil, err
        }
        if len(ips) == 0 {
            return nil, fmt.Errorf("webhook host %q has no DNS records", host)
        }
        for _, item := range ips {
            ip, ok := netip.AddrFromSlice(item.IP)
            if !ok || !permittedWebhookIP(ip) {
                return nil, fmt.Errorf("webhook host %q resolves to a prohibited address", host)
            }
        }
        var dialErr error
        for _, item := range ips {
            conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
            if err == nil {
                return conn, nil
            }
            dialErr = err
        }
        return nil, fmt.Errorf("webhook connection failed: %w", dialErr)
    }
}

// ValidateCallback 允许公网、内网和本地 HTTP/HTTPS 回调，不需要域名白名单。
func ValidateCallback(raw string) error {
    if len(raw) > 1024 || strings.TrimSpace(raw) != raw || raw == "" {
        return ErrInvalid
    }
    u, err := url.Parse(raw)
    if err != nil || u == nil || u.Hostname() == "" || u.User != nil ||
        u.Fragment != "" || u.Opaque != "" ||
        (u.Scheme != "http" && u.Scheme != "https") {
        return ErrInvalid
    }
    host := u.Hostname()
    if prohibitedMetadataHost(host) {
        return ErrInvalid
    }
    if ip, err := netip.ParseAddr(host); err == nil && !permittedWebhookIP(ip) {
        return ErrInvalid
    }
    if port := u.Port(); port != "" {
        number, err := strconv.Atoi(port)
        if err != nil || number < 1 || number > 65535 {
            return ErrInvalid
        }
    }
    return nil
}
