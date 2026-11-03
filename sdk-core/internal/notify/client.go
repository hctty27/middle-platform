// Package notify 负责向游戏服务器发送独立签名的发货通知。
package notify

import (
    "bytes"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/netip"
    "net/url"
    "strconv"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

var ErrTarget = errors.New("游戏发货通知目标地址不合法")

var blockedCallbackNetworks = []netip.Prefix{
    netip.MustParsePrefix("0.0.0.0/8"),
    netip.MustParsePrefix("240.0.0.0/4"),
    netip.MustParsePrefix("100.100.100.200/32"),
    netip.MustParsePrefix("fd00:ec2::254/128"),
}

func permittedCallbackIP(ip netip.Addr) bool {
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

// Client 向公网或内网的 HTTP/HTTPS 地址发送独立签名通知。
type Client struct {
    http *http.Client
}

// NewClient 直连实际解析的 IP，禁用重定向并拦截敏感地址。
func NewClient() *Client {
    transport := &http.Transport{
        DisableKeepAlives: true,
        TLSHandshakeTimeout: 3 * time.Second,
        ResponseHeaderTimeout: 4 * time.Second,
        DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
            host, port, err := net.SplitHostPort(address)
            if err != nil || prohibitedMetadataHost(host) {
                return nil, ErrTarget
            }
            resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
            if err != nil || len(resolved) == 0 {
                return nil, ErrTarget
            }
            for _, item := range resolved {
                ip, ok := netip.AddrFromSlice(item.IP)
                if !ok || !permittedCallbackIP(ip) {
                    return nil, ErrTarget
                }
            }
            dialer := &net.Dialer{Timeout: 3 * time.Second}
            var dialErr error
            for _, item := range resolved {
                conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
                if err == nil {
                    return conn, nil
                }
                dialErr = err
            }
            return nil, fmt.Errorf("发货通知连接失败: %w", dialErr)
        },
    }
    return &Client{
        http: &http.Client{
            Timeout: 5 * time.Second,
            Transport: transport,
            CheckRedirect: func(*http.Request, []*http.Request) error {
                return http.ErrUseLastResponse
            },
        },
    }
}

// ValidateTarget 不限制合法业务域名、IP 或端口。
func (c *Client) ValidateTarget(raw string) error {
    if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > 1024 {
        return ErrTarget
    }
    u, err := url.Parse(raw)
    if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
        u.User != nil || u.Hostname() == "" || u.Fragment != "" ||
        u.RawQuery != "" || u.ForceQuery || u.Opaque != "" {
        return ErrTarget
    }
    host := u.Hostname()
    if prohibitedMetadataHost(host) {
        return ErrTarget
    }
    if ip, err := netip.ParseAddr(host); err == nil && !permittedCallbackIP(ip) {
        return ErrTarget
    }
    if port := u.Port(); port != "" {
        number, err := strconv.Atoi(port)
        if err != nil || number < 1 || number > 65535 {
            return ErrTarget
        }
    }
    return nil
}

// Send 使用 SDK Core 与游戏间的 HMAC-SHA256，不转发平台密钥。
// V3 签名由 auth.SignWithKey 按接口规则统一封装，不依赖通知 URL。
func (c *Client) Send(ctx context.Context, o model.Order, key []byte) error {
    if err := c.ValidateTarget(o.GameNotifyURL); err != nil {
        return err
    }
    if len(key) != 32 {
        return errors.New("发货通知签名密钥不合法")
    }
    body, err := json.Marshal(model.PaymentCallback{
        OrderNo:o.PlatformOrderNo, GameOrderID:o.GameOrderID,
        Amount:o.Amount, Attach:o.Attach, PaidAt:o.PaidAt.UTC().Format(time.RFC3339),
    })
    if err != nil {
        return err
    }
    var nonceBytes [8]byte
    if _, err = rand.Read(nonceBytes[:]); err != nil {
        return err
    }
    nonce := hex.EncodeToString(nonceBytes[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    signature, err := auth.SignWithKey(key, auth.Message{
        Operation:auth.OperationPaymentNotify,Method:http.MethodPost,
        Env:o.Env,AppID:o.AppID,Timestamp:timestamp,Nonce:nonce,Body:body,
    })
    if err != nil {
        return err
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.GameNotifyURL, bytes.NewReader(body))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", o.AppID)
    req.Header.Set("x-env", o.Env)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    req.Header.Set("x-signature", signature)
    resp, err := c.http.Do(req)
    if err != nil {
        return fmt.Errorf("发货通知请求失败: %w", err)
    }
    defer resp.Body.Close()
    ack, err := io.ReadAll(io.LimitReader(resp.Body, 65))
    if err != nil {
        return fmt.Errorf("读取发货通知响应失败: %w", err)
    }
    if resp.StatusCode != http.StatusOK || string(ack) != nonce {
        return fmt.Errorf("游戏服未确认发货通知，HTTP 状态码=%d", resp.StatusCode)
    }
    return nil
}
