package platform

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
)

func TestEventClientForwardsWithoutBodySignature(t *testing.T) {
    body := []byte(`{"event":"login","ext":{"openId":"u1","roleLevel":80}}`)
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/report/event" || r.Method != http.MethodPost {
            t.Errorf("平台请求路径或方法异常: %s %s", r.Method, r.URL.Path)
        }
        sent, _ := io.ReadAll(r.Body)
        if string(sent) != string(body) {
            t.Errorf("Body 被修改: %s", sent)
        }
        expected := auth.SignValues([]string{
            "platform-secret", "110006",
            r.Header.Get("x-timestamp"), r.Header.Get("x-nonce"),
        })
        if r.Header.Get("x-signature") != expected {
            t.Errorf("平台签名不符合四字段协议")
        }
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"code":0,"message":"ok","data":true}`))
    }))
    defer server.Close()
    client, err := NewEventClient(server.URL + "/report/event")
    if err != nil {
        t.Fatal(err)
    }
    if err := client.Send(context.Background(), "110006", "platform-secret", body); err != nil {
        t.Fatal(err)
    }
}

func TestEventClientRejectsPlatformFailure(t *testing.T) {
    for _, tc := range []struct {
        name string
        status int
        reply string
    }{
        {"HTTP 失败", 502, "bad gateway"},
        {"业务失败", 200, `{"code":123,"data":false}`},
        {"数据非 true", 200, `{"code":0,"data":false}`},
        {"JSON 非法", 200, "bad response"},
    } {
        t.Run(tc.name, func(t *testing.T) {
            server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(tc.status)
                _, _ = io.WriteString(w, tc.reply)
            }))
            defer server.Close()
            client, err := NewEventClient(server.URL + "/report/event")
            if err != nil {
                t.Fatal(err)
            }
            if err := client.Send(context.Background(), "110006", "secret", []byte("{}")); !errors.Is(err, ErrEventRejected) {
                t.Fatalf("expected rejected event: %v", err)
            }
        })
    }
}

func TestEventEndpointValidation(t *testing.T) {
    if _, err := NewEventClient(""); err != nil {
        t.Fatal(err)
    }
    for _, raw := range []string{
        "not-a-url", "file:///tmp/report/event", "https://user:pass@example.com/report/event",
        "https://example.com/other", "https://example.com/report/event?redirect=x",
    } {
        if _, err := NewEventClient(raw); err == nil {
            t.Errorf("接受了非法平台地址: %s", raw)
        }
    }
    client, _ := NewEventClient("")
    if err := client.Send(context.Background(), "app", "secret", nil); !errors.Is(err, ErrEventEndpointMissing) {
        t.Fatalf("expected missing endpoint: %v", err)
    }
}

func TestEventResponseDoesNotRequireMessage(t *testing.T) {
    var result struct {
        Code int `json:"code"`
        Data bool `json:"data"`
    }
    if err := json.NewDecoder(strings.NewReader(`{"code":0,"data":true}`)).Decode(&result); err != nil || result.Code != 0 || !result.Data {
        t.Fatalf("unexpected platform response: %+v %v", result, err)
    }
}
