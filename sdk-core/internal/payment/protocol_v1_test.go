package payment

import (
    "crypto/sha1"
    "encoding/hex"
    "fmt"
    "net/http"
    "sort"
    "strings"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)
func TestVerifyV1(t *testing.T){
    cb:=model.PaymentCallback{OrderNo:"p100",GameOrderID:"g100"}
    nonce:="0123456789abcdef";ts:=fmt.Sprint(time.Now().Unix());secret:="test_secret"
    values:=[]string{cb.OrderNo,cb.GameOrderID,secret,nonce,ts}
    sort.Strings(values);sum:=sha1.Sum([]byte(strings.Join(values,"_")))
    h:=http.Header{}
    h.Set("x-timestamp",ts);h.Set("x-nonce",nonce);h.Set("x-signature",hex.EncodeToString(sum[:]))
    if err:=VerifyV1(h,cb,secret);err!=nil{t.Fatal(err)}
    cb.OrderNo="different"
    if err:=VerifyV1(h,cb,secret);err==nil{t.Fatal("changed order accepted")}
}
