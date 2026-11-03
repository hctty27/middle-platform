package auth_test

import (
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
)

func TestGamePlatformStyleSignatureVectors(t *testing.T) {
    secret := []byte("测试-密钥!")
    const stamp = "1793700000"
    const nonce = "0123456789abcdef"
    cases := []struct {
        name, body, want string
    }{
        {
            "订单登记",
            `{"game_order_id":"G100","amount":600,"attach":""}`,
            "a8c30a586539b1d9cd7c5429421e3e88b6a02f6e",
        },
        {
            "发货通知",
            `{"order_no":"P100","game_order_id":"G100","amount":600,"attach":"","paid_at":"2026-11-03T00:00:00Z"}`,
            "ab4a3a41931cb6ffb0859fce3da65cc791ab677c",
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            actual, err := auth.SignGame(secret, "110006", stamp, nonce, []byte(tc.body))
            if err != nil { t.Fatal(err) }
            if actual != tc.want { t.Fatalf("signature = %s, expected %s", actual, tc.want) }
        })
    }
}

func TestGameSigningFiltersOnlyEmptyValues(t *testing.T) {
    body := []byte(`{"a":"","b":null,"c":0,"d":false,"e":{"x":1,"y":2}}`)
    got, err := auth.SignGame([]byte("k"), "app", "1793700000", "0123456789abcdef", body)
    if err != nil {t.Fatal(err)}
    compact := []byte(`{"a":"","b":null,"c":0,"d":false,"e": { "x": 1, "y": 2 }}`)
    again, err := auth.SignGame([]byte("k"), "app", "1793700000", "0123456789abcdef", compact)
    if err != nil || got != again {t.Fatalf("nested JSON compacting mismatch: %s / %s: %v",got,again,err)}
    if _, err := auth.SignGame([]byte("k"), "app", "1793700000", "0123456789abcdef", []byte("null")); err == nil {
        t.Fatal("null body accepted")
    }
    if _, err := auth.SignGame(nil, "app", "1793700000", "0123456789abcdef", body); err == nil {
        t.Fatal("missing secret accepted")
    }
}

func TestVerifyRegistrationValidatesSignedFieldsAndRequestHeaders(t *testing.T) {
    secret := []byte("secret")
    body := []byte(`{"game_order_id":"G100","amount":600,"attach":"roleId=1"}`)
    stamp := strconv.FormatInt(time.Now().Unix(),10)
    nonce := "0123456789abcdef"
    signature, err := auth.SignGame(secret, "110006", stamp, nonce, body)
    if err != nil {t.Fatal(err)}
    req := httptest.NewRequest(http.MethodPost, "/sdk/api/core/test/110006/orders", nil)
    req.SetPathValue("env","test")
    req.SetPathValue("appid","110006")
    req.Header.Set("x-app-id","110006")
    req.Header.Set("x-timestamp",stamp)
    req.Header.Set("x-nonce",nonce)
    req.Header.Set("x-signature",signature)
    verify := func(raw []byte) error {return auth.VerifyRegistration(req,raw,secret)}
    if err := verify(body); err != nil {t.Fatal(err)}
    modified := []byte(`{"game_order_id":"G100","amount":700,"attach":"roleId=1"}`)
    if err := verify(modified); err == nil {t.Fatal("tampered amount accepted")}
    // env 不参与签名：这里更换路由环境不会影响验签。
    // 业务层负责校验 AppID 与目标环境绑定、限制订单跨环境登记。
    req.SetPathValue("env","prod")
    if err := verify(body); err != nil {t.Fatalf("env should not be signed: %v", err)}
    req.SetPathValue("env","test")
    req.SetPathValue("appid","another")
    if err := verify(body); err == nil {t.Fatal("cross-app replay accepted")}
    req.SetPathValue("appid","110006")
    req.Header.Set("x-timestamp",strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(),10))
    if err := verify(body); err == nil {t.Fatal("expired request accepted")}
    req.Header.Set("x-timestamp",stamp)
    req.Header.Set("x-nonce","bad")
    if err := verify(body); err == nil {t.Fatal("bad nonce accepted")}
    req.Header.Set("x-nonce",nonce)
    req.Header.Set("x-signature","BAD")
    if err := verify(body); err == nil {t.Fatal("bad signature format accepted")}
    req.Header.Set("x-signature",signature)
    req.Header.Del("x-app-id")
    if err := verify(body); err == nil {t.Fatal("missing appid header accepted")}
    // 代理路径不参加签名，外部添加 /sdk 前缀不影响验签。
    other := httptest.NewRequest(http.MethodPost, "/api/core/test/110006/orders", nil)
    other.SetPathValue("env","test")
    other.SetPathValue("appid","110006")
    other.Header = req.Header.Clone()
    other.Header.Set("x-app-id","110006")
    if err := auth.VerifyRegistration(other,body,secret); err != nil {t.Fatal(err)}
}
