package rolecontrol

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/transport"
)

const maxControlResponseBytes = 16 * 1024

type HTTPDoer = transport.HTTPDoer

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
    raw, err := transport.PostJSON(ctx, a.Client, target.RoleControlURL, target, payload, input.SignatureValues(), maxControlResponseBytes)
    if err != nil { return err }
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
