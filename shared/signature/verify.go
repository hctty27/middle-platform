package signature

import (
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const requestWindow = 5 * time.Minute

// ValidHeaders 验证现有平台/游戏协议的秒级时间戳、16 位小写 nonce 和 40 位小写签名。
// 不改变业务层的 AppID、环境、请求方法及字段集合校验。
func ValidHeaders(timestamp, nonce, signed string, now time.Time) bool {
	if len(nonce) != 16 || len(signed) != 40 ||
		strings.ToLower(nonce) != nonce || strings.ToLower(signed) != signed {
		return false
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return false
	}
	if _, err := hex.DecodeString(signed); err != nil {
		return false
	}
	stamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	t := time.Unix(stamp, 0)
	return now.Sub(t) <= requestWindow && t.Sub(now) <= requestWindow
}

// EqualHex 比较服务端计算出的十六进制摘要与请求签名，不区分业务字段来源。
// 保留协议对小写形式的要求：大小写不同即为不匹配。
func EqualHex(expected, provided string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}
