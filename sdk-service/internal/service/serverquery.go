package service

import (
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/serverquery"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

// ServerQueryService 负责区服查询的服务路由、平台验签与适配器调用。
type ServerQueryService struct {
    Routes   ServiceRouteReader
    Adapters *serverquery.Registry
}

// Query 使用请求实际提交的字段集合验签，再调用对应游戏的区服查询适配器。
func (s *ServerQueryService) Query(r *http.Request, env, appID string, in model.ServerQueryRequest) (model.ServerQueryResult, error) {
    target, err := s.Routes.LookupService(r.Context(), appID, env, "server_query")
    if err != nil {
        return model.ServerQueryResult{}, err
    }
    if err := platform.VerifyFields(r, appID, target.AppSecret, time.Now(), in.SignatureValues()...); err != nil {
        return model.ServerQueryResult{}, err
    }
    implementation, err := s.Adapters.Get(target.AdapterName)
    if err != nil {
        return model.ServerQueryResult{}, err
    }
    return implementation.Query(r.Context(), target, in)
}
