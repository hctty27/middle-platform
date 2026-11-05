package rolecontrol

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

const maxControlResponseBytes = 16 * 1024

type HTTPDoer interface {
    Do(*http.Request) (*http.Response, error)
}

type Standard struct {
    Client HTTPDoer
}

func (a *Standard) Execute(ctx context.Context, target model.Target, input model.RoleControlRequest) error {
    if a.Client == nil {
        return fmt.Errorf("%w: 封解禁 HTTP 客户端未配置", model.ErrInvalidConfig)
    }
    payload, err := json.Marshal(input)
    if err != nil {
        return fmt.Errorf("%w: 序列化封解禁请求失败: %v", model.ErrInvalidConfig, err)
    }
    var random [8]byte
    if _, err := rand.Read(random[:]); err != nil {
        return fmt.Errorf("生成封解禁 nonce 失败: %w", err)
    }
    nonce := hex.EncodeToString(random[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.RoleControlURL, bytes.NewReader(payload))
    if err != nil {
        return fmt.Errorf("%w: 创建封解禁请求失败: %v", model.ErrInvalidConfig, err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", target.AppID)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    values := append([]string{target.GameSecret, target.AppID, timestamp, nonce}, input.SignatureValues()...)
    req.Header.Set("x-signature", signature.SignValues(values...))

    resp, err := a.Client.Do(req)
    if err != nil {
        return fmt.Errorf("%w: %w", model.ErrUpstream, err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("%w: 游戏封解禁返回 HTTP %d", model.ErrUpstream, resp.StatusCode)
    }
    raw, err := io.ReadAll(io.LimitReader(resp.Body, maxControlResponseBytes+1))
    if err != nil || len(raw) > maxControlResponseBytes {
        return fmt.Errorf("%w: 游戏封解禁响应读取失败或超过大小限制", model.ErrUpstream)
    }
    var output struct {
        Code *int `json:"code"`
        Message *string `json:"message"`
        Data json.RawMessage `json:"data"`
    }
    if json.Unmarshal(raw, &output) != nil || output.Code == nil || *output.Code != 0 ||
        output.Message == nil || string(output.Data) != "null" {
        return fmt.Errorf("%w: 游戏封解禁返回失败或响应不符合标准协议", model.ErrUpstream)
    }
    return nil
}

var _ Adapter = (*Standard)(nil)
