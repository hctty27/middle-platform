package service

import (
    "context"
    "crypto/sha1"
    "encoding/hex"
    "errors"
    "net/http"
    "sort"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type asyncOrderRepo struct{
    mu sync.Mutex
    order model.Order
    confirmations int
}
func (f *asyncOrderRepo) Find(_ context.Context, appID,gameOrderID string)(model.Order,error){
    f.mu.Lock();defer f.mu.Unlock()
    if f.order.AppID!=appID||f.order.GameOrderID!=gameOrderID{return model.Order{},model.ErrOrderNotFound}
    return f.order,nil
}
func (f *asyncOrderRepo) Register(context.Context,model.Order)error{return errors.New("unexpected registration")}
func (f *asyncOrderRepo) ConfirmPayment(_ context.Context,id int64,platformNo string,paidAt time.Time)error{
    f.mu.Lock();defer f.mu.Unlock()
    if f.order.ID!=id{return model.ErrOrderNotFound}
    if f.order.PaymentStatus==1&&f.order.PlatformOrderNo!=platformNo{return model.ErrOrderConflict}
    f.order.PaymentStatus=1
    f.order.PlatformOrderNo=platformNo
    f.order.PaidAt=paidAt
    f.confirmations++
    return nil
}
func (f *asyncOrderRepo) CompleteNotify(context.Context,int64,int,error)error{
    return errors.New("callback must not send game notification")
}
func (f *asyncOrderRepo) ClaimDue(context.Context)(*model.Order,error){
    return nil,errors.New("callback must not claim notification")
}

type asyncPlatformSecret struct{}
func (asyncPlatformSecret) LookupPlatformSecret(context.Context,string)(string,error){
    return "test-platform-secret",nil
}

// TestConcurrentCallbacksOnlyConfirmPayment 验证重复回调不竞争通知租约，也不等待游戏服务。
func TestConcurrentCallbacksOnlyConfirmPayment(t *testing.T){
    orders:=&asyncOrderRepo{order:model.Order{
        ID:17,AppID:"app1001",GameOrderID:"game-1",Env:"prod",Amount:600,Attach:"item-1",
    }}
    svc:=&OrderService{Orders:orders,PlatformSecretReader:asyncPlatformSecret{}}
    cb:=model.PaymentCallback{OrderNo:"platform-1",GameOrderID:"game-1",Amount:600,
        Attach:"item-1",PaidAt:"2026-11-02T13:00:00Z"}
    ts:=strconv.FormatInt(time.Now().Unix(),10)
    nonce:="0123456789abcdef"
    parts:=[]string{cb.OrderNo,cb.GameOrderID,"test-platform-secret",nonce,ts}
    sort.Strings(parts)
    signature:=sha1.Sum([]byte(strings.Join(parts,"_")))
    header:=http.Header{}
    header.Set("x-timestamp",ts)
    header.Set("x-nonce",nonce)
    header.Set("x-signature",hex.EncodeToString(signature[:]))

    const requests=32
    var wg sync.WaitGroup
    errorsCh:=make(chan error,requests)
    for i:=0;i<requests;i++{
        wg.Add(1)
        go func(){
            defer wg.Done()
            returned,err:=svc.Receive(context.Background(),"app1001",header,cb)
            if err!=nil{errorsCh<-err;return}
            if returned!=nonce{errorsCh<-errors.New("invalid callback acknowledgment")}
        }()
    }
    wg.Wait()
    close(errorsCh)
    for err:=range errorsCh{t.Fatal(err)}
    orders.mu.Lock()
    defer orders.mu.Unlock()
    if orders.order.PaymentStatus!=1||orders.order.PlatformOrderNo!="platform-1"||orders.confirmations!=requests{
        t.Fatalf("callback state mismatch: %+v confirmations=%d",orders.order,orders.confirmations)
    }
}
