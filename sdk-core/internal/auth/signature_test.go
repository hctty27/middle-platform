package auth_test

import (
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
)

func TestJavaCompatibleSortedMD5Vector(t *testing.T){
    data:=map[string]any{"b":2,"a":" x ","flag":false,"empty":"","skip":nil,"sign":"ignored"}
    got,err:=auth.Sign(data,[]byte("My Secret"))
    if err!=nil{t.Fatal(err)}
    const want="701D9CD53A47A6F49F147230805FDB9C"
    if got!=want{t.Fatalf("got %s, want %s",got,want)}
    if _,err:=auth.Sign(data,nil);err==nil{t.Fatal("empty secret accepted")}
    if _,err:=auth.Sign(map[string]any{"payload":map[string]any{"nested":1}},[]byte("s"));err==nil{
        t.Fatal("nested values must be pre-serialized")
    }
}

func TestSignRequestVector(t *testing.T){
    body:=[]byte(`{"game_order_id":"G100","amount":600,"attach":"roleId=1"}`)
    sig,err:=auth.SignRequest([]byte("secret"),auth.OperationOrderRegister,
        "test","110006","1793700000","0123456789abcdef",body)
    if err!=nil{t.Fatal(err)}
    const want="B4AB77CDEC0116260192CEAC17D88D1D"
    if sig!=want{t.Fatalf("got %s, want %s",sig,want)}
    if _,err:=auth.SignRequest([]byte("secret"),auth.OperationOrderRegister,
        "test","110006","1793700000","0123456789abcdef",
        []byte(`{"game_order_id":"G100","action":"custom"}`));err==nil{
        t.Fatal("request body shadowed action")
    }
}

func TestRegistrationVerification(t *testing.T){
    body:=[]byte(`{"game_order_id":"G100","amount":600,"attach":"roleId=1"}`)
    secret:=[]byte("secret")
    timestamp:=strconv.FormatInt(time.Now().Unix(),10)
    nonce:="0123456789abcdef"
    signature,err:=auth.SignRequest(secret,auth.OperationOrderRegister,
        "test","110006",timestamp,nonce,body)
    if err!=nil{t.Fatal(err)}
    req:=httptest.NewRequest(http.MethodPost,"/sdk/api/core/test/110006/orders",nil)
    req.SetPathValue("env","test")
    req.SetPathValue("appid","110006")
    req.Header.Set("x-app-id","110006")
    req.Header.Set("x-env","test")
    req.Header.Set("x-timestamp",timestamp)
    req.Header.Set("x-nonce",nonce)
    req.Header.Set("x-signature",signature)
    verify:=func(raw []byte)error{return auth.VerifyRegistration(req,raw,secret)}
    if err:=verify(body);err!=nil{t.Fatalf("valid signature rejected: %v",err)}
    if err:=verify([]byte(`{"game_order_id":"G100","amount":700,"attach":"roleId=1"}`));err==nil {
        t.Fatal("amount tamper accepted")
    }
    req.SetPathValue("env","prod")
    if err:=verify(body);err==nil{t.Fatal("cross-env accepted")}
    req.SetPathValue("env","test")
    req.SetPathValue("appid","other")
    if err:=verify(body);err==nil{t.Fatal("cross-app accepted")}
    req.SetPathValue("appid","110006")
    req.Header.Set("x-timestamp",strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(),10))
    if err:=verify(body);err==nil{t.Fatal("stale signature accepted")}
    req.Header.Set("x-timestamp",timestamp)
    req.Header.Set("x-nonce","invalid")
    if err:=verify(body);err==nil{t.Fatal("invalid nonce accepted")}
    req.Header.Set("x-nonce",nonce)
    req.Header.Set("x-signature","bad")
    if err:=verify(body);err==nil{t.Fatal("invalid signature accepted")}
}
