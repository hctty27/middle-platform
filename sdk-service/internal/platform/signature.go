// Package platform 负责验证平台发出的用户核验请求。
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

// 核验接口暂按平台主动请求的 Body 顶层值签名，待平台提供实测向量后锁定。
func VerifyRequest(r *http.Request, appID, secret, openID string, now time.Time) error {
    ts := r.Header.Get("x-timestamp")
    nonce := r.Header.Get("x-nonce")
    signed := r.Header.Get("x-signature")
    if appID == "" || secret == "" || openID == "" || r.Header.Get("x-app-id") != appID ||
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
    expected, _ := hex.DecodeString(signature.SignValues(secret, appID, ts, nonce, openID))
    if subtle.ConstantTimeCompare(expected, signedBytes) != 1 { return ErrUnauthorized }
    return nil
}
