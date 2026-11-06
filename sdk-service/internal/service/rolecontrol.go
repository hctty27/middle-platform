package service

import (
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/rolecontrol"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

// RoleControlService 负责封解禁请求的服务路由、验签与游戏适配。
type RoleControlService struct {
    Routes   ServiceRouteReader
    Adapters *rolecontrol.Registry
}

// Execute 使用平台约定字段验签，并将封解禁操作交给对应游戏适配器。
func (s *RoleControlService) Execute(r *http.Request, env, appID string, input model.RoleControlRequest) error {
    target, err := s.Routes.LookupService(r.Context(), appID, env, "role_control")
    if err != nil {
        return err
    }
    if err := platform.VerifyFields(r, appID, target.AppSecret, time.Now(), input.SignatureValues()...); err != nil {
        return err
    }
    implementation, err := s.Adapters.Get(target.AdapterName)
    if err != nil {
        return err
    }
    return implementation.Execute(r.Context(), target, input)
}
