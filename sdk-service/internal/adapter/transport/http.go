package transport

import (
    "bytes"
    "context"
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "io"
    "net/http"
    "strconv"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/shared/signature"
)

type HTTPDoer interface {
    Do(*http.Request) (*http.Response, error)
}

// PostJSON 使用统一的游戏侧签名协议发送业务请求；调用方保留各自的签名字段和响应校验。
func PostJSON(ctx context.Context, client HTTPDoer, url string, target model.Target,
    payload []byte, businessValues []string, maxResponseBytes int64) ([]byte, error) {
    if client == nil {
        return nil, fmt.Errorf("%w: HTTP 客户端未配置", model.ErrInvalidConfig)
    }
    var random [8]byte
    if _, err := rand.Read(random[:]); err != nil {
        return nil, fmt.Errorf("生成游戏请求 nonce 失败: %w", err)
    }
    nonce := hex.EncodeToString(random[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
    if err != nil {
        return nil, fmt.Errorf("%w: 创建游戏请求失败: %v", model.ErrInvalidConfig, err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", target.AppID)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    values := append([]string{target.GameSecret, target.AppID, timestamp, nonce}, businessValues...)
    req.Header.Set("x-signature", signature.SignValues(values...))

    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("%w: %w", model.ErrUpstream, err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("%w: 游戏返回 HTTP %d", model.ErrUpstream, resp.StatusCode)
    }
    raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
    if err != nil || int64(len(raw)) > maxResponseBytes {
        return nil, fmt.Errorf("%w: 游戏响应读取失败或超过大小限制", model.ErrUpstream)
    }
    return raw, nil
}

// NewHTTPClient 复用连接池并拒绝重定向，防止向未经配置的目标转发游戏签名。
func NewHTTPClient(timeout time.Duration) *http.Client {
    return &http.Client{
        Timeout: timeout,
        CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
        Transport: &http.Transport{
            Proxy: http.ProxyFromEnvironment,
            MaxIdleConns: 50,
            MaxIdleConnsPerHost: 10,
            IdleConnTimeout: 90*time.Second,
            TLSHandshakeTimeout: 3*time.Second,
            ResponseHeaderTimeout: timeout,
        },
    }
}
