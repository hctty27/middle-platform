// Package auth 验证游戏服务器向 SDK Service 发起的业务请求。
package auth

import (
	"errors"
	"net/http"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

var ErrUnauthorized = errors.New("游戏请求签名验证失败")

func VerifyGameRequest(r *http.Request, appID, secret string, body []byte, now time.Time) error {
	if !signature.VerifyGameHMAC(r, []byte(secret), appID, body, now) {
		return ErrUnauthorized
	}
	return nil
}
