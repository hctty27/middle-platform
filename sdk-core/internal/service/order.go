package service

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "regexp"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
    "github.com/hctty27/middle-platform/sdk-core/internal/notify"
    "github.com/hctty27/middle-platform/sdk-core/internal/payment"
    "github.com/hctty27/middle-platform/sdk-core/internal/store"
)

var ErrUnsafePlatformProtocol = errors.New("platform amount verification not available")
var ErrInvalidOrder = errors.New("invalid order request")
var orderIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type OrderService struct {
    ConfigReader Reader
    Orders *store.OrderStore
    GameAuth *auth.GameAuth
    PlatformSecrets map[string]map[string]string
    Notifier *notify.Client
    AllowUnverifiedNonProd bool
}

func(s *OrderService) VerifyRegistration(r *http.Request,body []byte,env,appid string)error {
    if s.GameAuth==nil{return auth.ErrUnauthorized}
    return s.GameAuth.Verify(r,body,env,appid)
}

func validEnv(env string)bool {return env=="prod"||env=="test"||env=="review"}

func(s *OrderService) Register(ctx context.Context,env,appid string,req model.RegisterOrder)error {
    if !validEnv(env)||!orderIDPattern.MatchString(appid)||
        !orderIDPattern.MatchString(req.GameOrderID)||req.Amount<=0||
        req.Amount>9007199254740991||len(req.Attach)>4096 {
        return ErrInvalidOrder
    }
    // 已存在订单的重试不依赖当前大区通知地址，避免配置变更破坏幂等。
    previous, lookupErr := s.Orders.Find(ctx, appid, req.GameOrderID)
    if lookupErr == nil {
        if previous.Env != env || previous.Amount != req.Amount || previous.Attach != req.Attach {
            return model.ErrOrderConflict
        }
        return nil
    }
    if !errors.Is(lookupErr, model.ErrOrderNotFound) { return lookupErr }
    _,group,err:=s.ConfigReader.Lookup(ctx,appid,env)
    if err!=nil{return err}
    if group==nil {return model.ErrNotFound}
    var config map[string]json.RawMessage
    if err=json.Unmarshal(group.RegionConfig,&config);err!=nil{return model.ErrInvalidConfig}
    raw,ok:=config["payNotifyUrl"]
    if !ok{return model.ErrInvalidConfig}
    var url string
    if err=json.Unmarshal(raw,&url);err!=nil||url==""{return model.ErrInvalidConfig}
    if err=s.Notifier.ValidateTarget(url);err!=nil{return model.ErrInvalidConfig}
    return s.Orders.Register(ctx,model.Order{
        AppID:appid,Env:env,GameOrderID:req.GameOrderID,
        Amount:req.Amount,Attach:req.Attach,GameNotifyURL:url,
    })
}

// Receive 先读取候选订单确定环境，再按该环境使用平台密钥验签。
// v1 未签 amount：仅允许明确启用的非正式环境联调。
func(s *OrderService) Receive(ctx context.Context,appid string,h http.Header,
    callback model.PaymentCallback)(string,error){
    if !orderIDPattern.MatchString(appid)||!orderIDPattern.MatchString(callback.GameOrderID)||
        !orderIDPattern.MatchString(callback.OrderNo)||callback.Amount<=0 {
        return "",ErrInvalidOrder
    }
    o,err:=s.Orders.Find(ctx,appid,callback.GameOrderID)
    if err!=nil{return "",err}
    secret:=s.PlatformSecrets[o.Env][appid]
    if err=payment.VerifyV1(h,callback,secret);err!=nil{return "",err}
    if callback.Amount!=o.Amount||callback.Attach!=o.Attach {return "",model.ErrOrderConflict}
    paidAt,err:=time.Parse(time.RFC3339,callback.PaidAt)
    if err!=nil{return "",ErrInvalidOrder}
    if o.Env=="prod"||!s.AllowUnverifiedNonProd {return "",ErrUnsafePlatformProtocol}
    if err=s.Orders.ConfirmPayment(ctx,o.ID,callback.OrderNo,paidAt);err!=nil{return "",err}
    updated,err:=s.Orders.ByID(ctx,o.ID)
    if err!=nil{return "",err}
    if updated.NotifyStatus==1 {return h.Get("x-nonce"),nil}
    claimed,err:=s.Orders.BeginNotify(ctx,o.ID)
    if err!=nil{return "",err}
    if !claimed {
        state,stateErr:=s.Orders.ByID(ctx,o.ID)
        if stateErr==nil&&state.NotifyStatus==1 {return h.Get("x-nonce"),nil}
        return "",model.ErrOrderNotPaid
    }
    if err=s.deliver(ctx,updated);err!=nil{return "",err}
    return h.Get("x-nonce"),nil
}

func(s *OrderService) deliver(ctx context.Context,o model.Order)error {
    sendCtx,cancel:=context.WithTimeout(ctx,5*time.Second)
    defer cancel()
    err:=s.Notifier.Send(sendCtx,o)
    commitCtx,commitCancel:=context.WithTimeout(context.Background(),2*time.Second)
    defer commitCancel()
    if saveErr:=s.Orders.CompleteNotify(commitCtx,o.ID,err);saveErr!=nil {
        return fmt.Errorf("persist notification result: %w",saveErr)
    }
    return err
}

// RunWorker 使用持久化任务时间戳补偿已支付未通知的订单。
func(s *OrderService) RunWorker(ctx context.Context) {
    ticker:=time.NewTicker(2*time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():return
        case <-ticker.C:
            jobCtx,cancel:=context.WithTimeout(ctx,3*time.Second)
            order,err:=s.Orders.ClaimDue(jobCtx)
            cancel()
            if err!=nil||order==nil{continue}
            _=s.deliver(ctx,*order)
        }
    }
}

func(s *OrderService) Validate()error {
    if s.Orders==nil||s.ConfigReader==nil||s.Notifier==nil||s.GameAuth==nil {
        return errors.New("order service dependency missing")
    }
    if len(s.GameAuth.Keys)==0||len(s.PlatformSecrets)==0{return errors.New("order auth secrets missing")}
    for id,c:=range s.GameAuth.Keys {
        if strings.TrimSpace(id)==""||len(c.Secret)<32||len(c.AppIDs)==0||len(c.Envs)==0 {
            return fmt.Errorf("invalid game auth credential: %s",id)
        }
    }
    return nil
}
