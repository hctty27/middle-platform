package auth_test

import (
    "crypto/hkdf"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "net/http"
    "net/http/httptest"
    "sort"
    "strconv"
    "strings"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
)

func independentSignature(t *testing.T,master []byte,purpose string,values ...string)string{
    t.Helper()
    key,err:=hkdf.Key(sha256.New,master,nil,"middle-platform:sdk-core:"+purpose+":v3",32)
    if err!=nil{t.Fatal(err)}
    sort.Strings(values)
    mac:=hmac.New(sha256.New,key)
    _,_=mac.Write([]byte(strings.Join(values,"_")))
    return hex.EncodeToString(mac.Sum(nil))
}

func TestRegistrationV3VectorAndTamperRejection(t *testing.T){
    secret:=[]byte("自定义密钥-123!@#")
    body:=[]byte(`{"game_order_id":"G100","amount":600,"attach":"roleId=1"}`)
    timestamp:=strconv.FormatInt(time.Now().Unix(),10)
    nonce:="0123456789abcdef"
    hash:=sha256.Sum256(body)
    expected:=independentSignature(t,secret,auth.PurposeGameRequest,
        "110006","test","POST","order.register",timestamp,nonce,hex.EncodeToString(hash[:]),"G100")
    actual,err:=auth.Sign(secret,auth.PurposeGameRequest,auth.Message{
        Operation:auth.OperationOrderRegister,Method:http.MethodPost,
        Env:"test",AppID:"110006",Timestamp:timestamp,Nonce:nonce,Body:body,
    })
    if err!=nil||actual!=expected{t.Fatalf("V3 signature mismatch: %s != %s; %v",actual,expected,err)}
    req:=httptest.NewRequest(http.MethodPost,"/proxy/api/core/test/110006/orders",nil)
    req.SetPathValue("env","test")
    req.SetPathValue("appid","110006")
    req.Header.Set("x-app-id","110006")
    req.Header.Set("x-env","test")
    req.Header.Set("x-timestamp",timestamp)
    req.Header.Set("x-nonce",nonce)
    req.Header.Set("x-signature",actual)
    verify:=func(body []byte)error{return auth.VerifyRegistration(req,body,secret)}
    if err:=verify(body);err!=nil{t.Fatal(err)}
    if err:=verify([]byte(`{"game_order_id":"G100","amount":700,"attach":"roleId=1"}`));err==nil{
        t.Fatal("tampered amount accepted")
    }
    req.SetPathValue("env","prod")
    if err:=verify(body);err==nil{t.Fatal("cross-env replay accepted")}
    req.SetPathValue("env","test")
    req.SetPathValue("appid","different")
    if err:=verify(body);err==nil{t.Fatal("cross-app replay accepted")}
    req.SetPathValue("appid","110006")
    req.Header.Set("x-timestamp",strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(),10))
    if err:=verify(body);err==nil{t.Fatal("expired request accepted")}
    req.Header.Set("x-timestamp",timestamp)
    req.Header.Set("x-nonce","invalid")
    if err:=verify(body);err==nil{t.Fatal("invalid nonce accepted")}
}

func TestNotificationV3VectorAndRules(t *testing.T){
    secret:=[]byte("自定义密钥-123!@#")
    body:=[]byte(`{"order_no":"P100","game_order_id":"G100","amount":600,"attach":""}`)
    ts,nonce:="1793700000","0123456789abcdef"
    hash:=sha256.Sum256(body)
    expected:=independentSignature(t,secret,auth.PurposeGameNotify,
        "110006","prod","POST","payment.notify",ts,nonce,hex.EncodeToString(hash[:]),"P100")
    key,err:=auth.Derive(secret,auth.PurposeGameNotify)
    if err!=nil{t.Fatal(err)}
    actual,err:=auth.SignWithKey(key,auth.Message{
        Operation:auth.OperationPaymentNotify,Method:http.MethodPost,
        Env:"prod",AppID:"110006",Timestamp:ts,Nonce:nonce,Body:body,
    })
    if err!=nil||actual!=expected{t.Fatalf("notification: %s != %s; %v",actual,expected,err)}
    if _,err:=auth.SignWithKey([]byte("short"),auth.Message{Operation:auth.OperationPaymentNotify});err==nil{
        t.Fatal("invalid key accepted")
    }
    if _,err:=auth.Sign(nil,auth.PurposeGameRequest,auth.Message{Operation:auth.OperationOrderRegister});err==nil{
        t.Fatal("empty secret accepted")
    }
    if _,err:=auth.Sign(key,auth.PurposeGameRequest,auth.Message{Operation:"unknown"});err==nil{
        t.Fatal("unknown operation accepted")
    }
    a,_:=auth.SigningFields(auth.OperationOrderRegister)
    b,_:=auth.SigningFields(auth.OperationPaymentNotify)
    if strings.Join(a,",")==strings.Join(b,","){t.Fatal("per-interface rules were not loaded")}
}
