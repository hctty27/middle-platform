package service

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "strings"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

var ErrInvalidEvent = errors.New("事件上报参数不合法")
var ErrServiceDisabled = errors.New("当前渠道环境未开启事件上报")

// EventSender 将已验证的事件发送到指定渠道的平台接口。
type EventSender interface {
    Send(context.Context, string, string, []byte) error
}

// EventService 只处理渠道包事件：验签、服务开关和同步转发，不保存原始事件。
type EventService struct {
    ConfigReader Reader
    SwitchReader interface {
        LookupSDKService(context.Context, string, string, string) (bool, json.RawMessage, error)
    }
    Sender EventSender
}

func (s *EventService) Validate() error {
    if s.ConfigReader == nil || s.SwitchReader == nil || s.Sender == nil {
        return errors.New("事件上报服务依赖未配置完整")
    }
    return nil
}

// Report 从 x-app-id 定位渠道，校验绑定与签名后检查双重开关，再同步上报。
func (s *EventService) Report(r *http.Request, env, appID string, body []byte) error {
    if !validEnv(env) || !orderIDPattern.MatchString(appID) || r.Header.Get("x-app-id") != appID {
        return ErrInvalidEvent
    }
    app, group, err := s.ConfigReader.Lookup(r.Context(), appID, env)
    if err != nil {
        return err
    }
    if group == nil || app.Code != appID {
        return model.ErrNotFound
    }
    gameSecret, err := auth.ParseGameSecret(app.GameConfig)
    if err != nil {
        return model.ErrInvalidConfig
    }
    if err := auth.VerifyGameRequest(r, body, gameSecret, env, appID); err != nil {
        return err
    }

    var payload struct {
        Event string          `json:"event"`
        Ext   json.RawMessage `json:"ext"`
    }
    if err := json.Unmarshal(body, &payload); err != nil ||
        len(payload.Event) == 0 || len(payload.Event) > 128 ||
        strings.TrimSpace(payload.Event) != payload.Event ||
        len(payload.Ext) == 0 || !bytes.HasPrefix(bytes.TrimSpace(payload.Ext), []byte("{")) {
        return ErrInvalidEvent
    }

    enabled, _, err := s.SwitchReader.LookupSDKService(r.Context(), appID, env, "event_report")
    if err != nil {
        return err
    }
    if !enabled {
        return ErrServiceDisabled
    }

    var channelConfig struct {
        AppSecret string `json:"appSecret"`
    }
    if err := json.Unmarshal(app.Config, &channelConfig); err != nil ||
        strings.TrimSpace(channelConfig.AppSecret) == "" {
        return model.ErrInvalidConfig
    }
    return s.Sender.Send(r.Context(), appID, channelConfig.AppSecret, body)
}
