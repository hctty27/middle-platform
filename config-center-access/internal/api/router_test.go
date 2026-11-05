package api

import (
    "bytes"
    "fmt"
    "net/http"
    "net/http/httptest"
    "sync/atomic"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

type fakeTrigger struct { count atomic.Int64 }
func (f *fakeTrigger) Request() { f.count.Add(1) }

func TestCallbackSignatureAndAcceptance(t *testing.T) {
    trigger := &fakeTrigger{}
    handler := Router(nil, trigger, "test-secret")
    body := []byte(`{"epoch":"an-epoch","revision":42}`)
    timestamp := fmt.Sprint(time.Now().Unix())
    nonce := "0123456789abcdef"
    signed := signature.SignValues("test-secret", timestamp, nonce, "an-epoch", "42")
    freshNonce := "fedcba9876543210"
    fresh := signature.SignValues("test-secret", timestamp, freshNonce, "an-epoch", "42")
    tests := []struct {
        name string
        body []byte
        timestamp, nonce, signed string
        want int
    }{
        {"valid",body,timestamp,nonce,signed,http.StatusAccepted},
        {"replay",body,timestamp,nonce,signed,http.StatusConflict},
        {"invalid",body,timestamp,"0011223344556677","bad",http.StatusUnauthorized},
        {"old",body,timestamp,"0011223344556677","sha256=old",http.StatusUnauthorized},
        {"expired",body,fmt.Sprint(time.Now().Add(-10*time.Minute).Unix()),"0011223344556677",signed,http.StatusUnauthorized},
        {"missing-nonce",body,timestamp,"",signed,http.StatusUnauthorized},
        {"tampered-body",[]byte(`{"epoch":"changed","revision":42}`),timestamp,"0011223344556677",signed,http.StatusUnauthorized},
        {"fresh-retry",body,timestamp,freshNonce,fresh,http.StatusAccepted},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            req := httptest.NewRequest(http.MethodPost,"/internal/config/callback",bytes.NewReader(tc.body))
            req.Header.Set("X-Timestamp",tc.timestamp)
            req.Header.Set("X-Nonce",tc.nonce)
            req.Header.Set("X-Signature",tc.signed)
            w := httptest.NewRecorder()
            handler.ServeHTTP(w,req)
            if w.Code != tc.want { t.Fatalf("HTTP %d, 期望 %d: %s", w.Code,tc.want,w.Body.String()) }
        })
    }
    if got:=trigger.count.Load();got!=2 { t.Fatalf("触发 %d 次，期望 2 次",got) }
}

func TestConfigWebhookSignatureFixture(t *testing.T) {
    got:=signature.SignValues("demo-webhook-secret","1793865600","0123456789abcdef",
        "550e8400-e29b-41d4-a716-446655440000","12")
    if want:="a95aee794bee55d020586fd60c8b0fe860779636";got!=want {
        t.Fatalf("签名向量错误: got %s, want %s",got,want)
    }
}

func TestCallbackRejectsMalformedJSON(t *testing.T) {
    trigger:=&fakeTrigger{}
    w:=httptest.NewRecorder()
    req:=httptest.NewRequest(http.MethodPost,"/internal/config/callback",bytes.NewBufferString("{bad"))
    Router(nil,trigger,"secret").ServeHTTP(w,req)
    if w.Code!=http.StatusBadRequest || trigger.count.Load()!=0 { t.Fatal(w.Code) }
    if !bytes.Contains(w.Body.Bytes(),[]byte("JSON 格式不合法")) { t.Fatal(w.Body.String()) }
}
