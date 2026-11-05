package serverquery

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/transport"
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
    raw, err := transport.PostJSON(ctx, a.Client, target.ServerQueryURL, target, payload, in.ForwardSignatureValues(), maxServerResponseBytes)
    if err != nil { return model.ServerQueryResult{}, err }
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
