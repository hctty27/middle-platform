// Package platform 验证平台主动发起的服务请求。
package platform

import (
    "crypto/subtle"
    "encoding/hex"
    "errors"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

var ErrUnauthorized = errors.New("平台请求签名校验失败")

// VerifyRequest 保留用户核验接口的调用方式。
func VerifyRequest(r *http.Request, appID, secret, openID string, now time.Time) error {
    if openID == "" {
        return ErrUnauthorized
    }
    return VerifyFields(r, appID, secret, now, openID)
}

// VerifyFields 按平台协议将 Header 四项与 Body 顶层所有非空值参与签名。
func VerifyFields(r *http.Request, appID, secret string, now time.Time, bodyValues ...string) error {
    ts := r.Header.Get("x-timestamp")
    nonce := r.Header.Get("x-nonce")
    signed := r.Header.Get("x-signature")
    if appID == "" || secret == "" || r.Header.Get("x-app-id") != appID ||
        len(nonce) != 16 || strings.ToLower(nonce) != nonce ||
        len(signed) != 40 || strings.ToLower(signed) != signed {
        return ErrUnauthorized
    }
    nonceBytes, nonceErr := hex.DecodeString(nonce)
    signedBytes, signedErr := hex.DecodeString(signed)
    stamp, timeErr := strconv.ParseInt(ts, 10, 64)
    if nonceErr != nil || len(nonceBytes) != 8 || signedErr != nil || timeErr != nil ||
        now.Sub(time.Unix(stamp, 0)) > 5*time.Minute ||
        time.Unix(stamp, 0).Sub(now) > 5*time.Minute {
        return ErrUnauthorized
    }
    values := append([]string{secret, appID, ts, nonce}, bodyValues...)
    expected, _ := hex.DecodeString(signature.SignValues(values...))
    if subtle.ConstantTimeCompare(expected, signedBytes) != 1 { return ErrUnauthorized }
    return nil
}
