// Package auth 中的签名工具统一游戏侧 V2 协议；支付平台 V1 由 payment 包独立处理。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// signV2 统一对原始请求体求 SHA256，并用已派生密钥计算 HMAC-SHA256。
// fields 保持业务协议中的顺序；每项以 LF 分隔，最后追加 body hash，不带结尾换行。
func signV2(derivedKey []byte, fields []string, rawBody []byte) ([]byte, error) {
	if len(derivedKey) != keyLength {
		return nil, ErrGameSecret
	}
	sum := sha256.Sum256(rawBody)
	payload := strings.Join(append(fields, hex.EncodeToString(sum[:])), "\n")
	mac := hmac.New(sha256.New, derivedKey)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil), nil
}

func registrationMAC(masterSecret []byte, env, appID, timestamp string, rawBody []byte) ([]byte, error) {
	key, err := Derive(masterSecret, PurposeOrderRegister)
	if err != nil {
		return nil, err
	}
	return signV2(key, []string{http.MethodPost, env, appID, timestamp}, rawBody)
}

// SignRegistration 使用订单登记 V2 协议签名；签名包含 env/appid，不包含 URL 路径。
func SignRegistration(masterSecret []byte, env, appID, timestamp string, rawBody []byte) (string, error) {
	sum, err := registrationMAC(masterSecret, env, appID, timestamp, rawBody)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum), nil
}

// SignNotification 使用已按 payment-notify:v2 派生的密钥签名发货通知。
// 不依赖通知 URL；由调用方传入随机 nonce 和秒级时间戳。
func SignNotification(derivedKey []byte, appID, timestamp, nonce string, rawBody []byte) (string, error) {
	sum, err := signV2(derivedKey, []string{appID, http.MethodPost, timestamp, nonce}, rawBody)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sum), nil
}

// VerifyRegistration 校验订单登记签名、五分钟时间窗口和业务标识。
func VerifyRegistration(r *http.Request, rawBody, masterSecret []byte) error {
	timestamp := r.Header.Get("X-Game-Timestamp")
	signature := r.Header.Get("X-Game-Signature")
	stamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(stamp, 0)) > 5*time.Minute ||
		time.Until(time.Unix(stamp, 0)) > 5*time.Minute {
		return ErrUnauthorized
	}
	if len(signature) != 64 || strings.ToLower(signature) != signature {
		return ErrUnauthorized
	}
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return ErrUnauthorized
	}
	env, appID := r.PathValue("env"), r.PathValue("appid")
	if env == "" || appID == "" {
		return ErrUnauthorized
	}
	expected, err := registrationMAC(masterSecret, env, appID, timestamp, rawBody)
	if err != nil || !hmac.Equal(provided, expected) {
		return ErrUnauthorized
	}
	return nil
}
