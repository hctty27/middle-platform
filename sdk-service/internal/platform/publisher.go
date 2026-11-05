package platform

import (
    "bytes"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

var ErrPublishEndpointMissing = errors.New("平台接收地址未配置")
var ErrPublishRejected = errors.New("平台拒绝请求或平台调用失败")

type Publisher struct {
    client *http.Client
    endpoints map[string]string
}

func NewPublisher(eventURL, subscribeURL string) (*Publisher, error) {
    endpoints := map[string]string{
        "event_report": eventURL,
        "subscribe_message": subscribeURL,
    }
    paths := map[string]string{
        "event_report": "/report/event",
        "subscribe_message": "/game/subscribe/send-message",
    }
    for code, raw := range endpoints {
        if raw == "" {
            continue
        }
        u, err := url.Parse(raw)
        if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
            u.Hostname() == "" || u.User != nil || u.RawQuery != "" ||
            u.Fragment != "" || u.Opaque != "" || u.Path != paths[code] ||
            strings.TrimSpace(raw) != raw {
            return nil, fmt.Errorf("平台 %s 接口地址不合法", code)
        }
    }
    return &Publisher{
        endpoints: endpoints,
        client: &http.Client{
            Timeout: 5 * time.Second,
            CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
        },
    }, nil
}

func (p *Publisher) Send(ctx context.Context, code, appID, secret string, body []byte) error {
    if p == nil || p.endpoints[code] == "" {
        return ErrPublishEndpointMissing
    }
    if code != "event_report" && code != "subscribe_message" {
        return ErrPublishEndpointMissing
    }
    if appID == "" || secret == "" {
        return errors.New("平台签名密钥或 AppID 缺失")
    }
    var bytesNonce [8]byte
    if _, err := rand.Read(bytesNonce[:]); err != nil {
        return fmt.Errorf("生成平台请求 nonce 失败: %w", err)
    }
    nonce := hex.EncodeToString(bytesNonce[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoints[code], bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("%w: 创建请求失败: %v", ErrPublishRejected, err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", appID)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    // 平台开放服务仅签 Header 四值，不包含业务 Body。
    req.Header.Set("x-signature", signature.SignValues(secret, appID, timestamp, nonce))
    response, err := p.client.Do(req)
    if err != nil {
        return fmt.Errorf("%w: %w", ErrPublishRejected, err)
    }
    defer response.Body.Close()
    raw, err := io.ReadAll(io.LimitReader(response.Body, 8*1024+1))
    if err != nil || len(raw) > 8*1024 || response.StatusCode != http.StatusOK {
        return fmt.Errorf("%w: 平台 HTTP %d 或响应读取失败", ErrPublishRejected, response.StatusCode)
    }
    var result struct {
        Code *int `json:"code"`
        Data *bool `json:"data"`
    }
    if json.Unmarshal(raw, &result) != nil || result.Code == nil || *result.Code != 0 ||
        result.Data == nil || !*result.Data {
        return fmt.Errorf("%w: 平台业务返回不成功", ErrPublishRejected)
    }
    return nil
}
