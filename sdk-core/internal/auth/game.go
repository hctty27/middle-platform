// Package auth 统一游戏主密钥解析、HKDF 派生以及游戏侧 V2 签名与验签。
package auth

import (
    "crypto/hkdf"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "strings"
)

const (
    PurposeOrderRegister = "order-register"
    PurposePaymentNotify = "payment-notify"
    keyLength = 32
)

// ErrUnauthorized 表示游戏服登记请求签名或时间戳无效。
var ErrUnauthorized = errors.New("invalid game signature")
// ErrGameSecret 表示游戏主密钥缺失或为空。
var ErrGameSecret = errors.New("gameSecret must be a non-empty string")

// ParseGameSecret 接受非空 UTF-8 字符串，并兼容旧版标准 Base64 的 32 字节密钥。
// 仅对严格标准 Base64 且解码后恰好 32 字节的旧格式执行解码；其他内容按原始 UTF-8 字节使用。
func ParseGameSecret(raw json.RawMessage) ([]byte,error) {
    var cfg struct { GameSecret *string `json:"gameSecret"` }
    if err:=json.Unmarshal(raw,&cfg);err!=nil||cfg.GameSecret==nil||
        strings.TrimSpace(*cfg.GameSecret)=="" {return nil,ErrGameSecret}
    value:=*cfg.GameSecret
    if decoded,err:=base64.StdEncoding.Strict().DecodeString(value);err==nil&&
        len(decoded)==keyLength&&base64.StdEncoding.EncodeToString(decoded)==value {
        return decoded,nil
    }
    return []byte(value),nil
}

// Derive 仅为预设的两个方向产生 32 字节派生密钥，版本号属于协议的一部分。
func Derive(secret []byte,purpose string)([]byte,error) {
    if len(secret)==0{return nil,ErrGameSecret}
    if purpose!=PurposeOrderRegister&&purpose!=PurposePaymentNotify {
        return nil,errors.New("unsupported key purpose")
    }
    return hkdf.Key(sha256.New,secret,nil,"middle-platform:sdk-core:"+purpose+":v2",keyLength)
}

