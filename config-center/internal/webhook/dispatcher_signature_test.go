package webhook

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

func TestSendUsesSharedSignature(t *testing.T) {
    handled := false
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost { t.Errorf("Webhook 方法错误: %s", r.Method) }
        if r.Header.Get("X-Config-Timestamp") != "" || r.Header.Get("X-Config-Signature") != "" {
            t.Error("仍发送旧版 HMAC Header")
        }
        stamp := r.Header.Get("X-Timestamp")
        nonce := r.Header.Get("X-Nonce")
        signed := r.Header.Get("X-Signature")
        if !signature.ValidHeaders(stamp, nonce, signed, time.Now()) {
            t.Error("Webhook Header 无效")
        }
        var payload struct {
            Epoch string `json:"epoch"`
            Revision int64 `json:"revision"`
        }
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            t.Errorf("Webhook JSON 解码失败: %v", err)
        }
        if payload.Epoch != "epoch-1" || payload.Revision != 42 {
            t.Errorf("Webhook 内容错误: %+v", payload)
        }
        expected := signature.SignValues("webhook-test-secret", stamp, nonce, payload.Epoch, strconv.FormatInt(payload.Revision, 10))
        if !signature.EqualHex(expected, signed) {
            t.Errorf("Webhook 签名错误: got %s, want %s", signed, expected)
        }
        handled = true
        w.WriteHeader(http.StatusAccepted)
    }))
    defer server.Close()

    dispatcher := &Dispatcher{Client: server.Client()}
    err := dispatcher.Send(context.Background(), Delivery{
        URL: server.URL, Secret: "webhook-test-secret", Epoch: "epoch-1", Revision: 42,
    })
    if err != nil { t.Fatalf("Webhook 发送失败: %v", err) }
    if !handled { t.Fatal("Webhook 未被接收") }
}

func TestSendRejectsEmptyWebhookSecret(t *testing.T) {
    dispatcher := &Dispatcher{Client: http.DefaultClient}
    if err := dispatcher.Send(context.Background(), Delivery{URL:"http://localhost:8081/notify"}); err == nil {
        t.Fatal("空 Secret 不应发送回调")
    }
}
