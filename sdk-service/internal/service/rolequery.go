package service

import (
    "context"
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/rolequery"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

type ServiceRouteReader interface {
    LookupService(context.Context, string, string, string) (model.Target, error)
}

type RoleQueryService struct {
    Routes   ServiceRouteReader
    Adapters *rolequery.Registry
}

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
