// Package auth 提供与 Java 游戏服一致的 MD5 签名与验签。
package auth

import (
    "encoding/json"
    "errors"
    "strings"
)

var ErrUnauthorized = errors.New("游戏请求签名验证失败")
var ErrGameSecret = errors.New("gameSecret 必须是非空字符串")

// ParseGameSecret 直接使用原始 UTF-8 字符串作为密钥，不做 Base64 解码。
func ParseGameSecret(raw json.RawMessage) ([]byte,error) {
    var cfg struct{GameSecret *string `json:"gameSecret"`}
    if err:=json.Unmarshal(raw,&cfg);err!=nil||cfg.GameSecret==nil||
        strings.TrimSpace(*cfg.GameSecret)==""{return nil,ErrGameSecret}
    return []byte(*cfg.GameSecret),nil
}
