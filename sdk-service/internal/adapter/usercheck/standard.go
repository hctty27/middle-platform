package usercheck

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/transport"
)

type HTTPDoer = transport.HTTPDoer
type Standard struct { Client HTTPDoer }

func (a *Standard) Check(ctx context.Context, target model.Target, in model.UserCheckRequest) (model.UserCheckResult, error) {
    if a.Client == nil { return model.UserCheckResult{}, fmt.Errorf("%w: HTTP 客户端未配置", model.ErrInvalidConfig) }
    payload, err := json.Marshal(map[string]string{"open_id": in.OpenID})
    if err != nil { return model.UserCheckResult{}, err }
    raw, err := transport.PostJSON(ctx, a.Client, target.UserCheckURL, target, payload, []string{in.OpenID}, 64*1024)
    if err != nil { return model.UserCheckResult{}, err }
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

func NewHTTPClient(timeout time.Duration) *http.Client { return transport.NewHTTPClient(timeout) }

var _ Adapter = (*Standard)(nil)
