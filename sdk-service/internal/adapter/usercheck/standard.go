package usercheck

import (
    "bytes"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strconv"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/shared/signature"
)

type HTTPDoer interface { Do(*http.Request) (*http.Response, error) }
type Standard struct { Client HTTPDoer }

func (a *Standard) Check(ctx context.Context, target model.Target, in model.UserCheckRequest) (model.UserCheckResult, error) {
    if a.Client == nil { return model.UserCheckResult{}, fmt.Errorf("%w: HTTP 客户端未配置", model.ErrInvalidConfig) }
    payload, err := json.Marshal(map[string]string{"open_id": in.OpenID})
    if err != nil { return model.UserCheckResult{}, err }
    var random [8]byte
    if _, err = rand.Read(random[:]); err != nil { return model.UserCheckResult{}, fmt.Errorf("生成游戏请求 nonce 失败: %w", err) }
    nonce := hex.EncodeToString(random[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.UserCheckURL, bytes.NewReader(payload))
    if err != nil { return model.UserCheckResult{}, fmt.Errorf("%w: 创建请求失败: %v", model.ErrInvalidConfig, err) }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", target.AppID)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    req.Header.Set("x-signature", signature.SignValues(target.GameSecret, target.AppID, timestamp, nonce, in.OpenID))
    resp, err := a.Client.Do(req)
    if err != nil { return model.UserCheckResult{}, fmt.Errorf("%w: %w", model.ErrUpstream, err) }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return model.UserCheckResult{}, fmt.Errorf("%w: HTTP %d", model.ErrUpstream, resp.StatusCode)
    }
    raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
    if err != nil || len(raw) > 64*1024 {
        return model.UserCheckResult{}, fmt.Errorf("%w: 游戏响应读取失败或超过限制", model.ErrUpstream)
    }
    var out struct {
        Code *int `json:"code"`
        Data *struct { Exists *bool `json:"exists"` } `json:"data"`
    }
    if json.Unmarshal(raw, &out) != nil || out.Code == nil || *out.Code != 0 ||
        out.Data == nil || out.Data.Exists == nil {
        return model.UserCheckResult{}, fmt.Errorf("%w: 游戏核验响应不符合标准协议", model.ErrUpstream)
    }
    return model.UserCheckResult{Exists: *out.Data.Exists}, nil
}

// NewHTTPClient 复用连接池并拒绝重定向，避免配置中的目标地址被上游改写。
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

var _ Adapter = (*Standard)(nil)
