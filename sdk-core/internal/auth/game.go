package auth

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "net/http"
    "strconv"
    "strings"
    "time"
)

var ErrUnauthorized = errors.New("invalid game credential")

type Credential struct {
    Secret string `yaml:"secret"`
    AppIDs []string `yaml:"appids"`
    Envs []string `yaml:"envs"`
}

type GameAuth struct { Keys map[string]Credential }

func inScope(values []string, item string) bool {
    for _,v:=range values {if v==item {return true}}
    return false
}

// Verify 对外部原始 path 与请求体哈希签名，鉴权范围与签名一致。
func (g *GameAuth) Verify(r *http.Request, body []byte, env, appID string) error {
    id:=r.Header.Get("X-Game-Key-Id")
    ts:=r.Header.Get("X-Game-Timestamp")
    sig:=r.Header.Get("X-Game-Signature")
    key,ok:=g.Keys[id]
    if !ok || len(key.Secret)<32 || !inScope(key.AppIDs,appID) || !inScope(key.Envs,env) {return ErrUnauthorized}
    n,err:=strconv.ParseInt(ts,10,64)
    if err!=nil || time.Since(time.Unix(n,0))>5*time.Minute || time.Until(time.Unix(n,0))>5*time.Minute {return ErrUnauthorized}
    if len(sig)!=64 || strings.ToLower(sig)!=sig {return ErrUnauthorized}
    supplied,err:=hex.DecodeString(sig)
    if err!=nil {return ErrUnauthorized}
    sum:=sha256.Sum256(body)
    canonical:=fmt.Sprintf("%s\n%s\n%s\n%s\n%x",id,r.Method,r.URL.EscapedPath(),ts,sum)
    mac:=hmac.New(sha256.New,[]byte(key.Secret))
    mac.Write([]byte(canonical))
    if !hmac.Equal(supplied,mac.Sum(nil)) {return ErrUnauthorized}
    return nil
}
