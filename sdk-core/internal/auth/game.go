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
// ErrGameSecret 表示游戏主密钥不满足 Base64 与长度要求。
var ErrGameSecret = errors.New("gameSecret must be base64 of 32 random bytes")

// ParseGameSecret 只接受 game.config 顶层 gameSecret，避免配置格式歧义。
func ParseGameSecret(raw json.RawMessage) ([]byte,error) {
    var cfg struct { GameSecret string `json:"gameSecret"` }
    if err:=json.Unmarshal(raw,&cfg);err!=nil{return nil,ErrGameSecret}
    secret,err:=base64.StdEncoding.Strict().DecodeString(cfg.GameSecret)
    if err!=nil||len(secret)!=keyLength{return nil,ErrGameSecret}
    return secret,nil
}

// Derive 仅为预设的两个方向产生 32 字节派生密钥，版本号属于协议的一部分。
func Derive(secret []byte,purpose string)([]byte,error) {
    if len(secret)!=keyLength{return nil,ErrGameSecret}
    if purpose!=PurposeOrderRegister&&purpose!=PurposePaymentNotify {
        return nil,errors.New("unsupported key purpose")
    }
    return hkdf.Key(sha256.New,secret,nil,"middle-platform:sdk-core:"+purpose+":v1",keyLength)
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
    payload:=fmt.Sprintf("%s\n%s\n%s\n%x",r.Method,r.URL.EscapedPath(),ts,sum)
    mac:=hmac.New(sha256.New,key)
    _,_=mac.Write([]byte(payload))
    if !hmac.Equal(supplied,mac.Sum(nil)){return ErrUnauthorized}
    return nil
}
