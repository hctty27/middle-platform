// Package auth 实现游戏侧的统一 SHA-1 签名与验证。
package auth

import (
    "encoding/json"
    "errors"
    "strings"
)

var ErrUnauthorized = errors.New("游戏请求签名验证失败")
var ErrGameSecret = errors.New("gameSecret 必须是非空字符串")

// ParseGameSecret 将游戏配置中的密钥原样作为 UTF-8 字符串，不再做 Base64 或 HKDF 处理。
func ParseGameSecret(raw json.RawMessage) ([]byte, error) {
    var cfg struct {
        GameSecret *string `json:"gameSecret"`
    }
    if err := json.Unmarshal(raw, &cfg); err != nil || cfg.GameSecret == nil ||
        strings.TrimSpace(*cfg.GameSecret) == "" {
        return nil, ErrGameSecret
    }
    return []byte(*cfg.GameSecret), nil
}
