// Package service 实现中台用户核验流程，屏蔽具体游戏协议。
package service

import (
    "context"
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/usercheck"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

type RouteReader interface {
    Lookup(context.Context, string, string) (model.Target, error)
}

type UserCheckService struct {
    Routes RouteReader
    Adapters *usercheck.Registry
}

func (s *UserCheckService) Check(r *http.Request, env, appID string, in model.UserCheckRequest) (model.UserCheckResult, error) {
    target, err := s.Routes.Lookup(r.Context(), appID, env)
    if err != nil { return model.UserCheckResult{}, err }
    if err := platform.VerifyRequest(r, appID, target.AppSecret, in.OpenID, time.Now()); err != nil {
        return model.UserCheckResult{}, err
    }
    adapter, err := s.Adapters.Get(target.AdapterName)
    if err != nil { return model.UserCheckResult{}, err }
    return adapter.Check(r.Context(), target, in)
}
