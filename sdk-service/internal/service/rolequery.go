package service

import (
    "context"
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/rolequery"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

// ServiceRouteReader 按渠道环境与服务代码提供业务转发目标。
type ServiceRouteReader interface {
    LookupService(context.Context, string, string, string) (model.Target, error)
}

// RoleQueryService 负责平台验签、路由查找与游戏角色查询适配。
type RoleQueryService struct {
    Routes   ServiceRouteReader
    Adapters *rolequery.Registry
}

// Query 先读取当前渠道环境的服务配置并验证平台签名，再调用对应游戏适配器。
func (s *RoleQueryService) Query(r *http.Request, env, appID string, in model.RoleQueryRequest) ([]model.Role, error) {
    target, err := s.Routes.LookupService(r.Context(), appID, env, "role_query")
    if err != nil {
        return nil, err
    }
    if err := platform.VerifyFields(r, appID, target.AppSecret, time.Now(), in.SignatureValues()...); err != nil {
        return nil, err
    }
    implementation, err := s.Adapters.Get(target.AdapterName)
    if err != nil {
        return nil, err
    }
    return implementation.Query(r.Context(), target, in)
}
