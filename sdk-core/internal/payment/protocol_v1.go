package payment

import (
    "crypto/sha1"
    "crypto/subtle"
    "encoding/hex"
    "errors"
    "net/http"
    "sort"
    "strconv"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// ErrSignature 表示支付平台 v1 签名或回调时间戳不合法。
var ErrSignature = errors.New("invalid platform callback signature")

// VerifyV1 严格匹配既有平台 v1：按五个值排序，以 _ 拼接，再做 SHA-1。
// 该签名未覆盖 amount/attach/paid_at，调用方不得把其视为金额认证。
func VerifyV1(h http.Header, body model.PaymentCallback, secret string) error {
    ts,nonce,sig:=h.Get("x-timestamp"),h.Get("x-nonce"),h.Get("x-signature")
    if len(secret)==0 || len(nonce)!=16 || len(sig)!=40 || strings.ToLower(nonce)!=nonce ||
        strings.ToLower(sig)!=sig {return ErrSignature}
    if _,err:=hex.DecodeString(nonce);err!=nil {return ErrSignature}
    incoming,err:=hex.DecodeString(sig)
    if err!=nil{return ErrSignature}
    n,err:=strconv.ParseInt(ts,10,64)
    if err!=nil || time.Since(time.Unix(n,0))>5*time.Minute ||
        time.Until(time.Unix(n,0))>5*time.Minute {return ErrSignature}
    values:=[]string{body.OrderNo,body.GameOrderID,secret,nonce,ts}
    sort.Strings(values)
    digest:=sha1.Sum([]byte(strings.Join(values,"_")))
    if subtle.ConstantTimeCompare(incoming,digest[:])!=1 {return ErrSignature}
    return nil
}
