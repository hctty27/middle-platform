package auth

import (
	"net/http"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

// VerifyGameRequest 验证游戏请求的 HMAC 签名及 AppID、环境路由。
func VerifyGameRequest(r *http.Request, rawBody, secret []byte, env, appID string) error {
	if r.Method != http.MethodPost || env == "" || r.PathValue("env") != env ||
		appID == "" || r.Header.Get("x-app-id") != appID {
		return ErrUnauthorized
	}
	if !signature.VerifyGameHMAC(r, secret, appID, rawBody, time.Now()) {
		return ErrUnauthorized
	}
	return nil
}

// VerifyRegistration 保留订单路径中的 AppID 约束。
func VerifyRegistration(r *http.Request, rawBody, secret []byte) error {
	return VerifyGameRequest(r, rawBody, secret, r.PathValue("env"), r.PathValue("appid"))
}
