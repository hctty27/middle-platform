package rolequery

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

const maxRoleResponseBytes = 2 * 1024 * 1024
const maxRoleCount = 1000

type HTTPDoer interface {
    Do(*http.Request) (*http.Response, error)
}

type Standard struct {
    Client HTTPDoer
}

func (a *Standard) Query(ctx context.Context, target model.Target, in model.RoleQueryRequest) ([]model.Role, error) {
    if a.Client == nil {
        return nil, fmt.Errorf("%w: 角色查询 HTTP 客户端未配置", model.ErrInvalidConfig)
    }
    payload, err := json.Marshal(in)
    if err != nil {
        return nil, fmt.Errorf("%w: 序列化角色查询请求失败: %v", model.ErrInvalidConfig, err)
    }
    var random [8]byte
    if _, err := rand.Read(random[:]); err != nil {
        return nil, fmt.Errorf("生成角色查询 nonce 失败: %w", err)
    }
    nonce := hex.EncodeToString(random[:])
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.RoleQueryURL, bytes.NewReader(payload))
    if err != nil {
        return nil, fmt.Errorf("%w: 创建角色查询请求失败: %v", model.ErrInvalidConfig, err)
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", target.AppID)
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    signedValues := append([]string{target.GameSecret, target.AppID, timestamp, nonce}, in.SignatureValues()...)
    req.Header.Set("x-signature", signature.SignValues(signedValues...))

    resp, err := a.Client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("%w: %w", model.ErrUpstream, err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("%w: 游戏角色查询返回 HTTP %d", model.ErrUpstream, resp.StatusCode)
    }
    raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRoleResponseBytes+1))
    if err != nil || len(raw) > maxRoleResponseBytes {
        return nil, fmt.Errorf("%w: 游戏角色查询响应读取失败或超过大小限制", model.ErrUpstream)
    }
    var output struct {
        Code *int `json:"code"`
        Data *[]model.Role `json:"data"`
    }
    if json.Unmarshal(raw, &output) != nil || output.Code == nil ||
        *output.Code != 0 || output.Data == nil || len(*output.Data) > maxRoleCount {
        return nil, fmt.Errorf("%w: 游戏角色查询响应不符合标准协议", model.ErrUpstream)
    }
    for _, role := range *output.Data {
        if !role.Valid() {
            return nil, fmt.Errorf("%w: 游戏角色信息缺少必填字段或类型不正确", model.ErrUpstream)
        }
    }
    return *output.Data, nil
}

var _ Adapter = (*Standard)(nil)
