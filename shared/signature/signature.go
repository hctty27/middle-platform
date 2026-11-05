// Package signature 提供平台兼容的按值排序 SHA-1 签名算法。
package signature

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
)

// SignValues 过滤空值后排序签名，供现有游戏及平台业务协议使用。
func SignValues(values ...string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	return signSorted(filtered)
}

// SignAllValues 不过滤空值，严格兼容支付回调 V1 的原有五字段算法。
func SignAllValues(values ...string) string {
	return signSorted(append([]string(nil), values...))
}

func signSorted(values []string) string {
	sort.Strings(values)
	digest := sha1.Sum([]byte(strings.Join(values, "_")))
	return hex.EncodeToString(digest[:])
}
