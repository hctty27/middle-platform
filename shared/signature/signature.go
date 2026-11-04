// Package signature 提供平台兼容的按值排序 SHA-1 签名算法。
package signature

import (
    "crypto/sha1"
    "encoding/hex"
    "sort"
    "strings"
)

func SignValues(values ...string) string {
    valuesCopy := make([]string, 0, len(values))
    for _, value := range values {
        if value != "" {
            valuesCopy = append(valuesCopy, value)
        }
    }
    sort.Strings(valuesCopy)
    digest := sha1.Sum([]byte(strings.Join(valuesCopy, "_")))
    return hex.EncodeToString(digest[:])
}
