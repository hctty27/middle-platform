package service

import (
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/serverquery"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

type ServerQueryService struct {
    Routes   ServiceRouteReader
    Adapters *serverquery.Registry
}

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
