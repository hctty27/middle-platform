package platform

import (
    "context"
    "io"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/hctty27/middle-platform/shared/signature"
)

func TestPublisherForwardsBothServices(t *testing.T) {
    payloads := map[string]string{
        "/report/event": `{"event":"login","ext":{"roleId":"r1"}}`,
        "/game/subscribe/send-message": `{"open_id":"u1","tpl_id":"tpl"}`,
    }
    gamePlatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        want, ok := payloads[r.URL.Path]
        body, _ := io.ReadAll(r.Body)
        if !ok || r.Method != http.MethodPost || string(body) != want {
            t.Errorf("平台收到的请求不合法: %s %s %s", r.Method, r.URL.Path, body)
        }
        expected := signature.SignValues("platform-secret", "110006",
            r.Header.Get("x-timestamp"), r.Header.Get("x-nonce"))
        if r.Header.Get("x-signature") != expected {
            t.Error("平台请求不得将 Body 签进签名")
        }
        _, _ = w.Write([]byte(`{"code":0,"message":"ok","data":true}`))
    }))
    defer gamePlatform.Close()
    client, err := NewPublisher(gamePlatform.URL+"/report/event",
        gamePlatform.URL+"/game/subscribe/send-message")
    if err != nil { t.Fatal(err) }
    for code, path := range map[string]string{
        "event_report": "/report/event",
        "subscribe_message": "/game/subscribe/send-message",
    } {
        if err := client.Send(context.Background(), code, "110006", "platform-secret",
            []byte(payloads[path])); err != nil {
            t.Fatalf("%s 转发失败: %v", code, err)
        }
    }
}

func TestPublisherResponseAndEndpointFailures(t *testing.T) {
    for _, tc := range []struct{ status int; result string }{
        {200, `{"code":500,"data":false}`},
        {200, `{"code":0,"data":false}`},
        {200, `{"data":true}`},
        {502, `{"code":0,"data":true}`},
        {200, `garbage`},
    } {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
            w.WriteHeader(tc.status)
            _, _ = w.Write([]byte(tc.result))
        }))
        p, err := NewPublisher(server.URL+"/report/event", "")
        if err != nil { t.Fatal(err) }
        if err := p.Send(context.Background(), "event_report", "app", "secret", []byte("{}")); err == nil {
            t.Errorf("失败平台响应被接受: %+v", tc)
        }
        server.Close()
    }
    p, err := NewPublisher("", "")
    if err != nil { t.Fatal(err) }
    if err := p.Send(context.Background(), "subscribe_message", "app", "secret", nil); err == nil {
        t.Fatal("缺少平台地址仍允许调用")
    }
    for _, invalid := range []string{
        "file:///report/event", "https://user:pass@example.com/report/event",
        "https://example.com/other", "https://example.com/report/event?q=1",
    } {
        if _, err := NewPublisher(invalid, ""); err == nil {
            t.Errorf("非法上报地址被接受: %s", invalid)
        }
    }
}
