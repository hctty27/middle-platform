// Package auth 验证游戏服务器向 SDK Service 发起的业务请求。
package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/hctty27/middle-platform/shared/signature"
)

var ErrUnauthorized = errors.New("游戏请求签名验证失败")

func SignGame(secret, appID, timestamp, nonce string, body []byte) (string, error) {
	if secret == "" || appID == "" || timestamp == "" || nonce == "" {
		return "", ErrUnauthorized
	}
	values, err := signature.BodyValues(body)
	if err != nil {
		return "", ErrUnauthorized
	}
	values = append(values, secret, appID, timestamp, nonce)
	return signature.SignValues(values...), nil
}

func VerifyGameRequest(r *http.Request, appID, secret string, body []byte, now time.Time) error {
	timestamp := r.Header.Get("x-timestamp")
	nonce := r.Header.Get("x-nonce")
	provided := r.Header.Get("x-signature")
	if r.Method != http.MethodPost || r.Header.Get("x-app-id") != appID ||
		!signature.ValidHeaders(timestamp, nonce, provided, now) {
		return ErrUnauthorized
	}
	expected, err := SignGame(secret, appID, timestamp, nonce, body)
	if err != nil || !signature.EqualHex(expected, provided) {
		return ErrUnauthorized
	}
	return nil
}
