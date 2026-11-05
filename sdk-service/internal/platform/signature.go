// Package platform 验证平台主动发起的服务请求。
package platform

import (
	"errors"
	"net/http"
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

// VerifyFields 保持平台协议：Header 四项与接口传入的业务字段值参与签名。
func VerifyFields(r *http.Request, appID, secret string, now time.Time, bodyValues ...string) error {
	ts := r.Header.Get("x-timestamp")
	nonce := r.Header.Get("x-nonce")
	provided := r.Header.Get("x-signature")
	if appID == "" || secret == "" || r.Header.Get("x-app-id") != appID ||
		!signature.ValidHeaders(ts, nonce, provided, now) {
		return ErrUnauthorized
	}
	values := append([]string{secret, appID, ts, nonce}, bodyValues...)
	expected := signature.SignValues(values...)
	if !signature.EqualHex(expected, provided) {
		return ErrUnauthorized
	}
	return nil
}
