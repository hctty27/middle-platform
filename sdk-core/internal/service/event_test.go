package service

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type eventConfigReader struct {
    app model.ChannelApp
    group *model.BoundGroup
    err error
}
func (f eventConfigReader) Lookup(context.Context, string, string) (model.ChannelApp, *model.BoundGroup, error) {
    return f.app, f.group, f.err
}

type eventSwitchReader struct {
    enabled bool
    env string
    appID string
    code string
}
func (f *eventSwitchReader) LookupSDKService(_ context.Context, appID, env, code string) (bool, json.RawMessage, error) {
    f.env, f.appID, f.code = env, appID, code
    return f.enabled, json.RawMessage("{}"), nil
}

type eventSender struct {
    calls int
    appID string
    secret string
    body []byte
}
func (f *eventSender) Send(_ context.Context, appID, secret string, body []byte) error {
    f.calls++
    f.appID, f.secret, f.body = appID, secret, append([]byte(nil), body...)
    return nil
}

func signedEventRequest(t *testing.T, body []byte) *http.Request {
    t.Helper()
    r := httptest.NewRequest(http.MethodPost, "/api/core/test/events", nil)
    r.SetPathValue("env", "test")
    ts := strconv.FormatInt(time.Now().Unix(), 10)
    nonce := "0123456789abcdef"
    sig, err := auth.SignGame([]byte("game-secret"), "110006", ts, nonce, body)
    if err != nil {
        t.Fatal(err)
    }
    r.Header.Set("x-app-id", "110006")
    r.Header.Set("x-timestamp", ts)
    r.Header.Set("x-nonce", nonce)
    r.Header.Set("x-signature", sig)
    return r
}

func TestEventReportChecksChannelSwitchAndForwards(t *testing.T) {
    body := []byte(`{"event":"login","ext":{"roleId":"r1","areaId":"a1"}}`)
    switches := &eventSwitchReader{enabled: true}
    sender := &eventSender{}
    svc := &EventService{
        ConfigReader: eventConfigReader{
            app: model.ChannelApp{
                Code: "110006",
                GameConfig: json.RawMessage(`{"gameSecret":"game-secret"}`),
                Config: json.RawMessage(`{"appSecret":"platform-secret"}`),
            },
            group: &model.BoundGroup{},
        },
        SwitchReader: switches,
        Sender: sender,
    }
    if err := svc.Validate(); err != nil {
        t.Fatal(err)
    }
    if err := svc.Report(signedEventRequest(t, body), "test", "110006", body); err != nil {
        t.Fatal(err)
    }
    if sender.calls != 1 || sender.appID != "110006" || sender.secret != "platform-secret" ||
        string(sender.body) != string(body) {
        t.Fatalf("转发不符合预期: %+v", sender)
    }
    if switches.appID != "110006" || switches.env != "test" || switches.code != "event_report" {
        t.Fatalf("开关查询参数不正确: %+v", switches)
    }

    switches.enabled = false
    if err := svc.Report(signedEventRequest(t, body), "test", "110006", body); !errors.Is(err, ErrServiceDisabled) {
        t.Fatalf("关闭事件开关仍允许上报: %v", err)
    }
    if sender.calls != 1 {
        t.Fatal("渠道关闭时仍然调用平台")
    }
    r := signedEventRequest(t, body)
    r.Header.Set("x-signature", "0000000000000000000000000000000000000000")
    if err := svc.Report(r, "test", "110006", body); !errors.Is(err, auth.ErrUnauthorized) {
        t.Fatalf("非法签名未拒绝: %v", err)
    }
    invalidExt := []byte(`{"event":"login","ext":"invalid"}`)
    if err := svc.Report(signedEventRequest(t, invalidExt), "test", "110006", invalidExt); !errors.Is(err, ErrInvalidEvent) {
        t.Fatalf("非对象 ext 未拒绝: %v", err)
    }
}
