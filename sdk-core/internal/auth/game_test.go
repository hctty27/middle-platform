package auth

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "fmt"
    "net/http/httptest"
    "testing"
    "time"
)

func testSecret()([]byte,string){
    secret:=make([]byte,32)
    for i:=range secret {secret[i]=byte(i)}
    return secret,base64.StdEncoding.EncodeToString(secret)
}

func TestHKDFPurposesAndGameConfig(t *testing.T){
    secret,encoded:=testSecret()
    parsed,err:=ParseGameSecret([]byte(fmt.Sprintf("{%q:%q}","gameSecret",encoded)))
    if err!=nil||!bytes.Equal(parsed,secret){t.Fatalf("parse: %v",err)}
    register,err:=Derive(parsed,PurposeOrderRegister)
    if err!=nil{t.Fatal(err)}
    notify,err:=Derive(parsed,PurposePaymentNotify)
    if err!=nil{t.Fatal(err)}
    repeat,err:=Derive(parsed,PurposeOrderRegister)
    if err!=nil{t.Fatal(err)}
    if len(register)!=32||len(notify)!=32||bytes.Equal(register,notify)||
        !bytes.Equal(register,repeat){t.Fatal("invalid HKDF key separation")}
    for _,raw:=range []string{"{}","{\"gameSecret\":\"\"}",
        "{\"gameSecret\":\"  \"}","{\"gameSecret\":null}","[\"wrong\"]",
        "{\"gameSecret\":42}"} {
        if _,err:=ParseGameSecret([]byte(raw));err==nil{t.Errorf("accepted %s",raw)}
    }
    // 不限编码与长度，短字符串、非 Base64 字符串及中文都能派生固定 32 字节密钥。
    for _,value:=range []string{"a","short","////","自定义密钥-123!@#",
        "very-long-game-secret-that-does-not-need-to-be-base64-encoded"} {
        raw:=[]byte(fmt.Sprintf("{%q:%q}","gameSecret",value))
        parsed,err:=ParseGameSecret(raw)
        if err!=nil||!bytes.Equal(parsed,[]byte(value)){t.Fatalf("plain secret %q: %v",value,err)}
        key,err:=Derive(parsed,PurposeOrderRegister)
        if err!=nil||len(key)!=32{t.Fatalf("derive %q: %v",value,err)}
    }
    // 保留首尾空格，不改变实际配置的密钥字节。
    spaced,err:=ParseGameSecret([]byte(`{"gameSecret":"  secret  "}`))
    if err!=nil||string(spaced)!="  secret  "{t.Fatalf("spaced secret changed: %v",err)}
    if _,err:=Derive(nil,PurposeOrderRegister);err==nil{t.Fatal("empty key must be rejected")}
    // 合法的旧版 32 字节 Base64 密钥在本测试开头已验证兼容。
    if _,err:=Derive(parsed,"unknown");err==nil{t.Fatal("unsupported purpose accepted")}
}

func TestVerifyRegistration(t *testing.T){
    master,_:=testSecret()
    key,err:=Derive(master,PurposeOrderRegister)
    if err!=nil{t.Fatal(err)}
    body:=[]byte("{\"game_order_id\":\"g100\",\"amount\":100,\"attach\":\"\"}")
    req:=httptest.NewRequest("POST","https://sdk.example.com/api/core/test/app1001/orders",nil)
    req.SetPathValue("env","test")
    req.SetPathValue("appid","app1001")
    stamp:=fmt.Sprint(time.Now().Unix())
    bodyHash:=sha256.Sum256(body)
    canonical:=fmt.Sprintf("%s\n%s\n%s\n%s\n%x","POST","test","app1001",stamp,bodyHash)
    mac:=hmac.New(sha256.New,key)
    mac.Write([]byte(canonical))
    req.Header.Set("X-Game-Timestamp",stamp)
    req.Header.Set("X-Game-Signature",hex.EncodeToString(mac.Sum(nil)))
    if err:=VerifyRegistration(req,body,master);err!=nil{t.Fatal(err)}
    if err:=VerifyRegistration(req,[]byte("modified"),master);err==nil{t.Fatal("tampered body accepted")}
    // 网关路径不同但业务标识一致，不应影响签名。
    same:=httptest.NewRequest("POST","https://sdk.example.com/gateway/api/core/test/app1001/orders",nil)
    same.SetPathValue("env","test")
    same.SetPathValue("appid","app1001")
    same.Header=req.Header.Clone()
    if err:=VerifyRegistration(same,body,master);err!=nil{t.Fatalf("path-dependent signature: %v",err)}
    other:=httptest.NewRequest("POST","https://sdk.example.com/api/core/prod/app1001/orders",nil)
    other.SetPathValue("env","prod")
    other.SetPathValue("appid","app1001")
    other.Header=req.Header.Clone()
    if err:=VerifyRegistration(other,body,master);err==nil{t.Fatal("cross-env replay accepted")}
    crossApp:=httptest.NewRequest("POST","https://sdk.example.com/api/core/test/another/orders",nil)
    crossApp.SetPathValue("env","test")
    crossApp.SetPathValue("appid","another")
    crossApp.Header=req.Header.Clone()
    if err:=VerifyRegistration(crossApp,body,master);err==nil{t.Fatal("cross-appid replay accepted")}
    req.Header.Set("X-Game-Timestamp",fmt.Sprint(time.Now().Add(-10*time.Minute).Unix()))
    if err:=VerifyRegistration(req,body,master);err==nil{t.Fatal("expired request accepted")}
}
