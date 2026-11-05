package auth

import (
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

func TestGameRequestSignature(t *testing.T) {
    body := []byte(`{"event":"login","ext":{"roleId":"100","roleLevel":80}}`)
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    nonce := "0011223344556677"
    // SHA-1 与旧 SDK Core 的值排序规则保持一致。
    expected := signature.SignValues("game-secret", "110006", timestamp, nonce,
        "login", `{"roleId":"100","roleLevel":80}`)
    got, err := SignGame("game-secret", "110006", timestamp, nonce, body)
    if err != nil || got != expected {
        t.Fatalf("事件签名不兼容：%s %s %v", got, expected, err)
    }
    req := httptest.NewRequest("POST", "/api/service/events/test", nil)
    req.Header.Set("x-app-id", "110006")
    req.Header.Set("x-timestamp", timestamp)
    req.Header.Set("x-nonce", nonce)
    req.Header.Set("x-signature", got)
    if err := VerifyGameRequest(req, "110006", "game-secret", body, time.Now()); err != nil {
        t.Fatal(err)
    }
    if err := VerifyGameRequest(req, "110006", "game-secret",
        []byte(`{"event":"logout","ext":{"roleId":"100","roleLevel":80}}`), time.Now()); err == nil {
        t.Fatal("篡改 Body 后未拒绝")
    }
    if err := VerifyGameRequest(req, "110006", "game-secret", body, time.Now().Add(6*time.Minute)); err == nil {
        t.Fatal("超时签名未拒绝")
    }
    if err := VerifyGameRequest(req, "another", "game-secret", body, time.Now()); err == nil {
        t.Fatal("跨渠道签名未拒绝")
    }
}

func TestSubscribeGameSignature(t *testing.T) {
    body := []byte(`{"open_id":"u1","tpl_id":"tpl","data":"{\\"thing1\\":{\\"value\\":\\"礼包\\"}}"}`)
    ts := "1793800000"
    nonce := "0011223344556677"
    got, err := SignGame("secret", "110006", ts, nonce, body)
    if err != nil || len(got) != 40 {
        t.Fatalf("订阅消息验签构建失败: %s %v", got, err)
    }
}
