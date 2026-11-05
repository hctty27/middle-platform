package serverquery

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

const maxServerResponseBytes = 2 * 1024 * 1024

type HTTPDoer interface {
    Do(*http.Request) (*http.Response, error)
}

type Standard struct {
    Client HTTPDoer
}

func (a *Standard) Query(ctx context.Context, target model.Target, in model.ServerQueryRequest) (model.ServerQueryResult, error) {
    if a.Client == nil {
        return model.ServerQueryResult{}, fmt.Errorf("%w: 区服查询 HTTP 客户端未配置", model.ErrInvalidConfig)
    }
    payload, err := json.Marshal(in)
    if err != nil {
        return model.ServerQueryResult{}, fmt.Errorf("%w: 序列化区服查询请求失败: %v", model.ErrInvalidConfig, err)
    }
    var random [8]byte
    if _, err := rand.Read(random[:]); err != nil {
        return model.ServerQueryResult{}, fmt.Errorf("生成区服查询 nonce 失败: %w", err)
    }
    nonce := hex.EncodeToString(random[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.ServerQueryURL, bytes.NewReader(payload))
    if err != nil {
        return model.ServerQueryResult{}, fmt.Errorf("%w: 创建区服查询请求失败: %v", model.ErrInvalidConfig, err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", target.AppID)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    values := append([]string{target.GameSecret, target.AppID, timestamp, nonce}, in.ForwardSignatureValues()...)
    req.Header.Set("x-signature", signature.SignValues(values...))
    resp, err := a.Client.Do(req)
    if err != nil {
        return model.ServerQueryResult{}, fmt.Errorf("%w: %w", model.ErrUpstream, err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return model.ServerQueryResult{}, fmt.Errorf("%w: 游戏区服查询返回 HTTP %d", model.ErrUpstream, resp.StatusCode)
    }
    raw, err := io.ReadAll(io.LimitReader(resp.Body, maxServerResponseBytes+1))
    if err != nil || len(raw) > maxServerResponseBytes {
        return model.ServerQueryResult{}, fmt.Errorf("%w: 游戏区服查询响应读取失败或超出限制", model.ErrUpstream)
    }
    var output struct {
        Code *int `json:"code"`
        Message *string `json:"message"`
        Data *struct {
            Total *int64 `json:"total"`
            List *[]model.ServerInfo `json:"list"`
        } `json:"data"`
    }
    if json.Unmarshal(raw, &output) != nil || output.Code == nil || *output.Code != 0 ||
        output.Message == nil || output.Data == nil || output.Data.Total == nil ||
        *output.Data.Total < 0 || output.Data.List == nil ||
        len(*output.Data.List) > in.PageSize || len(*output.Data.List) > 1000 ||
        int64(len(*output.Data.List)) > *output.Data.Total {
        return model.ServerQueryResult{}, fmt.Errorf("%w: 游戏区服查询响应不符合标准协议", model.ErrUpstream)
    }
    for _, server := range *output.Data.List {
        if !server.Valid() {
            return model.ServerQueryResult{}, fmt.Errorf("%w: 游戏区服信息缺少必填字段或类型不合法", model.ErrUpstream)
        }
    }
    return model.ServerQueryResult{Total: *output.Data.Total, List: *output.Data.List}, nil
}

var _ Adapter = (*Standard)(nil)
