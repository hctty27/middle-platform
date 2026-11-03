// Package auth 提供游戏主密钥校验、HKDF 派生以及订单登记验签。
package auth

import (
    "crypto/hkdf"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "strconv"
    "strings"
    "time"
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

// VerifyRegistration 校验签名、原始请求体和五分钟时钟偏差；权限从渠道包所属游戏绑定确定。
func VerifyRegistration(r *http.Request,body []byte,secret []byte)error{
    ts:=r.Header.Get("X-Game-Timestamp")
    sig:=r.Header.Get("X-Game-Signature")
    stamp,err:=strconv.ParseInt(ts,10,64)
    if err!=nil||time.Since(time.Unix(stamp,0))>5*time.Minute||
        time.Until(time.Unix(stamp,0))>5*time.Minute{return ErrUnauthorized}
    if len(sig)!=64||strings.ToLower(sig)!=sig{return ErrUnauthorized}
    supplied,err:=hex.DecodeString(sig)
    if err!=nil{return ErrUnauthorized}
    key,err:=Derive(secret,PurposeOrderRegister)
    if err!=nil{return ErrUnauthorized}
    sum:=sha256.Sum256(body)
    // 使用业务标识而非代理路径，避免 Nginx 重写导致签名不一致，同时阻断跨环境和跨 AppID 重放。
    env,appid:=r.PathValue("env"),r.PathValue("appid")
    if env==""||appid=="" {return ErrUnauthorized}
    payload:=fmt.Sprintf("%s\n%s\n%s\n%s\n%x",r.Method,env,appid,ts,sum)
    mac:=hmac.New(sha256.New,key)
    _,_=mac.Write([]byte(payload))
    if !hmac.Equal(supplied,mac.Sum(nil)){return ErrUnauthorized}
    return nil
}
