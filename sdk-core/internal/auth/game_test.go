package auth

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/http/httptest"
    "testing"
    "time"
)

func TestVerifyGameSignature(t *testing.T){
    secret:="01234567890123456789012345678901"
    g:=&GameAuth{Keys:map[string]Credential{"k1":{Secret:secret,AppIDs:[]string{"app1"},Envs:[]string{"test"}}}}
    raw:=[]byte(`{"game_order_id":"o1","amount":100,"attach":""}`)
    r:=httptest.NewRequest("POST","https://sdk.example.com/api/core/test/app1/orders",nil)
    ts:=fmt.Sprint(time.Now().Unix())
    digest:=sha256.Sum256(raw)
    canonical:=fmt.Sprintf("k1
POST
/api/core/test/app1/orders
%s
%x",ts,digest)
    mac:=hmac.New(sha256.New,[]byte(secret));mac.Write([]byte(canonical))
    r.Header.Set("X-Game-Key-Id","k1")
    r.Header.Set("X-Game-Timestamp",ts)
    r.Header.Set("X-Game-Signature",hex.EncodeToString(mac.Sum(nil)))
    if err:=g.Verify(r,raw,"test","app1");err!=nil{t.Fatal(err)}
    if err:=g.Verify(r,[]byte("changed"),"test","app1");err==nil{t.Fatal("modified body accepted")}
    if err:=g.Verify(r,raw,"prod","app1");err==nil{t.Fatal("wrong environment accepted")}
}
