package rolequery

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/transport"
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
    raw, err := transport.PostJSON(ctx, a.Client, target.RoleQueryURL, target, payload, in.SignatureValues(), maxRoleResponseBytes)
    if err != nil { return nil, err }
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
