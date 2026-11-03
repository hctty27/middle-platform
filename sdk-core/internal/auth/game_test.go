package auth

import (
    "bytes"
    "encoding/base64"
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
    register,err:=Derive(parsed,PurposeGameRequest)
    if err!=nil{t.Fatal(err)}
    notify,err:=Derive(parsed,PurposeGameNotify)
    if err!=nil{t.Fatal(err)}
    repeat,err:=Derive(parsed,PurposeGameRequest)
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
        key,err:=Derive(parsed,PurposeGameRequest)
        if err!=nil||len(key)!=32{t.Fatalf("derive %q: %v",value,err)}
    }
    // 保留首尾空格，不改变实际配置的密钥字节。
    spaced,err:=ParseGameSecret([]byte(`{"gameSecret":"  secret  "}`))
    if err!=nil||string(spaced)!="  secret  "{t.Fatalf("spaced secret changed: %v",err)}
    if _,err:=Derive(nil,PurposeGameRequest);err==nil{t.Fatal("empty key must be rejected")}
    // 合法的旧版 32 字节 Base64 密钥在本测试开头已验证兼容。
    if _,err:=Derive(parsed,"unknown");err==nil{t.Fatal("unsupported purpose accepted")}
}

func TestVerifyRegistration(t *testing.T){
    master,_:=testSecret()
    body:=[]byte(`{"game_order_id":"g100","amount":100,"attach":""}`)
    stamp:=fmt.Sprint(time.Now().Unix())
    nonce:="0123456789abcdef"
    req:=httptest.NewRequest("POST","https://sdk.example.com/api/core/test/app1001/orders",nil)
    req.SetPathValue("env","test")
    req.SetPathValue("appid","app1001")
    sig,err:=Sign(master,PurposeGameRequest,Message{
        Operation:OperationOrderRegister,Method:"POST",Env:"test",AppID:"app1001",
        Timestamp:stamp,Nonce:nonce,Body:body,
    })
    if err!=nil{t.Fatal(err)}
    req.Header.Set("x-app-id","app1001")
    req.Header.Set("x-env","test")
    req.Header.Set("x-timestamp",stamp)
    req.Header.Set("x-nonce",nonce)
    req.Header.Set("x-signature",sig)
    if err:=VerifyRegistration(req,body,master);err!=nil{t.Fatal(err)}
    if err:=VerifyRegistration(req,[]byte("modified"),master);err==nil{t.Fatal("tampering accepted")}
    same:=httptest.NewRequest("POST","https://sdk.example.com/gateway/api/core/test/app1001/orders",nil)
    same.SetPathValue("env","test")
    same.SetPathValue("appid","app1001")
    same.Header=req.Header.Clone()
    if err:=VerifyRegistration(same,body,master);err!=nil{t.Fatalf("proxy path should not matter: %v",err)}
}
