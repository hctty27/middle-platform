package service

import (
    "context"
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/auth"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type PlatformSender interface {
    Send(context.Context, string, string, string, []byte) error
}

type OutboundService struct {
    Routes ServiceRouteReader
    Sender PlatformSender
}

func (s *OutboundService) forward(r *http.Request, env, appID, code string, body []byte) error {
    target, err := s.Routes.LookupService(r.Context(), appID, env, code)
    if err != nil {
        return err
    }
    if target.AppID != appID || target.Env != env {
        return model.ErrInvalidConfig
    }
    if err := auth.VerifyGameRequest(r, appID, target.GameSecret, body, time.Now()); err != nil {
        return err
    }
    return s.Sender.Send(r.Context(), code, appID, target.AppSecret, body)
}

func (s *OutboundService) ReportEvent(r *http.Request, env, appID string, body []byte) error {
    return s.forward(r, env, appID, "event_report", body)
}

func (s *OutboundService) SendSubscribe(r *http.Request, env, appID string, body []byte) error {
    return s.forward(r, env, appID, "subscribe_message", body)
}
