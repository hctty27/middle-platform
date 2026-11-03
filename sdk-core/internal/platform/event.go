// Package platform 负责使用平台原生协议转发事件。
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

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
)

var ErrEventEndpointMissing = errors.New("未配置平台事件上报地址")
var ErrEventRejected = errors.New("平台事件上报失败")

// EventClient 复用 HTTP 连接并限制超时；平台地址只来自服务端 YAML。
type EventClient struct {
    endpoint string
    http *http.Client
}

func NewEventClient(endpoint string) (*EventClient, error) {
    if endpoint != "" {
        u, err := url.Parse(endpoint)
        if err != nil || u == nil ||
            (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
            u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" ||
            u.Path != "/report/event" || strings.TrimSpace(endpoint) != endpoint {
            return nil, errors.New("平台事件上报地址必须为完整的 HTTP/HTTPS /report/event 地址")
        }
    }
    return &EventClient{
        endpoint: endpoint,
        http: &http.Client{
            Timeout: 5 * time.Second,
            CheckRedirect: func(*http.Request, []*http.Request) error {
                return http.ErrUseLastResponse
            },
        },
    }, nil
}

// Send 平台入站签名只包含密钥、AppID、时间戳和 nonce，不包含事件 Body。
func (c *EventClient) Send(ctx context.Context, appID, secret string, body []byte) error {
    if c.endpoint == "" {
        return ErrEventEndpointMissing
    }
    if appID == "" || secret == "" {
        return errors.New("平台事件上报签名参数缺失")
    }
    var nonceBytes [8]byte
    if _, err := rand.Read(nonceBytes[:]); err != nil {
        return fmt.Errorf("生成事件 nonce 失败: %w", err)
    }
    nonce := hex.EncodeToString(nonceBytes[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    signature := auth.SignValues([]string{secret, appID, timestamp, nonce})

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("创建平台事件请求失败: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", appID)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    req.Header.Set("x-signature", signature)

    resp, err := c.http.Do(req)
    if err != nil {
        return fmt.Errorf("%w: 请求失败: %v", ErrEventRejected, err)
    }
    defer resp.Body.Close()
    raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
    if err != nil {
        return fmt.Errorf("%w: 读取平台响应失败: %v", ErrEventRejected, err)
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("%w: 平台 HTTP %d", ErrEventRejected, resp.StatusCode)
    }
    var result struct {
        Code int `json:"code"`
        Data bool `json:"data"`
    }
    if err := json.Unmarshal(raw, &result); err != nil || result.Code != 0 || !result.Data {
        return fmt.Errorf("%w: 平台业务响应不成功", ErrEventRejected)
    }
    return nil
}
