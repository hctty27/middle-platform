package service

import (
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/rolecontrol"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

type RoleControlService struct {
    Routes   ServiceRouteReader
    Adapters *rolecontrol.Registry
}

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
