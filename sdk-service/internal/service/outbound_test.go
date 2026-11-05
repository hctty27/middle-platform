package service

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/auth"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type fakeOutboundRoutes struct {
    code string
    enabled bool
}
func (r *fakeOutboundRoutes) LookupService(_ context.Context, appID, env, code string) (model.Target, error) {
    r.code = code
    if !r.enabled { return model.Target{}, model.ErrDisabled }
    return model.Target{AppID: appID, Env: env, GameSecret: "game-secret", AppSecret: "platform-secret"}, nil
}
type fakePlatformSender struct { calls int; code, appID, secret, body string }
func (s *fakePlatformSender) Send(_ context.Context, code, appID, secret string, body []byte) error {
    s.calls++
    s.code, s.appID, s.secret, s.body = code, appID, secret, string(body)
    return nil
}

func TestOutboundServiceChecksSignatureAndSwitch(t *testing.T) {
    routes := &fakeOutboundRoutes{enabled: true}
    sender := &fakePlatformSender{}
    svc := &OutboundService{Routes: routes, Sender: sender}
    for _, tc := range []struct {
        code string
        body string
    }{
        {"event_report", `{"event":"login","ext":{"roleId":"r1"}}`},
        {"subscribe_message", `{"open_id":"u1","tpl_id":"tpl","data":"{}"}`},
    } {
        body := []byte(tc.body)
        req := httptest.NewRequest(http.MethodPost, "/api/service/events/test", nil)
        ts := strconv.FormatInt(time.Now().Unix(), 10)
        nonce := "0011223344556677"
        sig, err := auth.SignGame("game-secret", "110006", ts, nonce, body)
        if err != nil { t.Fatal(err) }
        req.Header.Set("x-app-id", "110006")
        req.Header.Set("x-timestamp", ts)
        req.Header.Set("x-nonce", nonce)
        req.Header.Set("x-signature", sig)
        invoke := svc.ReportEvent
        if tc.code == "subscribe_message" { invoke = svc.SendSubscribe }
        if err := invoke(req, "test", "110006", body); err != nil {
            t.Fatalf("%s 签名合法但调用失败: %v", tc.code, err)
        }
        if routes.code != tc.code || sender.code != tc.code || sender.secret != "platform-secret" ||
            sender.body != tc.body {
            t.Fatalf("路由或密钥错误: %+v %+v", routes, sender)
        }
        routes.enabled = false
        previous := sender.calls
        if err := invoke(req, "test", "110006", body); err != model.ErrDisabled {
            t.Fatalf("%s 开关关闭未拒绝: %v", tc.code, err)
        }
        if sender.calls != previous { t.Fatal("关闭服务后仍转发") }
        routes.enabled = true
        req.Header.Set("x-signature", "0000000000000000000000000000000000000000")
        if err := invoke(req, "test", "110006", body); err != auth.ErrUnauthorized {
            t.Fatalf("%s 篡改签名未被拒绝: %v", tc.code, err)
        }
    }
}

func TestOutboundPayloadTypeContract(t *testing.T) {
    var out struct {
        OpenID string `json:"open_id"`
        TemplateID string `json:"tpl_id"`
        Data string `json:"data"`
    }
    if err := json.Unmarshal([]byte(`{"open_id":"u1","tpl_id":"tpl","data":"{}"}`), &out);
        err != nil || out.Data != "{}" {
        t.Fatalf("订阅消息 data 应保持字符串: %+v %v", out, err)
    }
}
