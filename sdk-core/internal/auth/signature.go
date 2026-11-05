package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hctty27/middle-platform/shared/signature"
)

// SignValues 保持旧调用方式，统一委托 shared/signature 计算。
func SignValues(values []string) string {
	return signature.SignValues(values...)
}

// SignGame 保留游戏协议签名字段：密钥、AppID、时间戳、Nonce 和 Body 顶层非空值。
// env 和 operation 不参与签名，由业务层另行校验。
func SignGame(secret []byte, appID, timestamp, nonce string, rawBody []byte) (string, error) {
	if len(secret) == 0 {
		return "", ErrGameSecret
	}
	if appID == "" || timestamp == "" || nonce == "" {
		return "", ErrUnauthorized
	}
	values, err := signature.BodyValues(rawBody)
	if err != nil {
		return "", fmt.Errorf("读取签名业务字段失败: %w", err)
	}
	values = append(values, string(secret), appID, timestamp, nonce)
	return signature.SignValues(values...), nil
}

// VerifyGameRequest 保持已有游戏验签协议，并校验 AppID 与环境路由。
func VerifyGameRequest(r *http.Request, rawBody, secret []byte, env, appID string) error {
	if r.Method != http.MethodPost || env == "" || r.PathValue("env") != env ||
		appID == "" || r.Header.Get("x-app-id") != appID {
		return ErrUnauthorized
	}
	ts := r.Header.Get("x-timestamp")
	nonce := r.Header.Get("x-nonce")
	provided := r.Header.Get("x-signature")
	if !signature.ValidHeaders(ts, nonce, provided, time.Now()) {
		return ErrUnauthorized
	}
	expected, err := SignGame(secret, appID, ts, nonce, rawBody)
	if err != nil || !signature.EqualHex(expected, provided) {
		return ErrUnauthorized
	}
	return nil
}

// VerifyRegistration 保留订单路径中的 AppID 约束。
func VerifyRegistration(r *http.Request, rawBody, secret []byte) error {
	return VerifyGameRequest(r, rawBody, secret, r.PathValue("env"), r.PathValue("appid"))
}
