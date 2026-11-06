package service

import (
    "context"
    "net/http"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/auth"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

// PlatformSender 定义向上游平台发送业务消息的能力。
type PlatformSender interface {
    Send(context.Context, string, string, string, []byte) error
}

// OutboundService 负责将游戏上报的事件和订阅消息转发给平台。
type OutboundService struct {
    Routes ServiceRouteReader
    Sender PlatformSender
}

// forward 先检查渠道服务开关和环境绑定，再校验游戏签名。
// 发往平台时使用渠道密钥重新签名，不透传游戏签名。
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

// ReportEvent 转发游戏事件上报请求。
func (s *OutboundService) ReportEvent(r *http.Request, env, appID string, body []byte) error {
    return s.forward(r, env, appID, "event_report", body)
}

// SendSubscribe 转发游戏订阅消息推送请求。
func (s *OutboundService) SendSubscribe(r *http.Request, env, appID string, body []byte) error {
    return s.forward(r, env, appID, "subscribe_message", body)
}
