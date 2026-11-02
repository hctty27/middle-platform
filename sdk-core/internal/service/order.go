package service

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "regexp"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
    "github.com/hctty27/middle-platform/sdk-core/internal/payment"
)

// ErrInvalidOrder 表示登记或回调的订单参数不符合约束。
var ErrInvalidOrder = errors.New("invalid order request")
var orderIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// OrderService 负责订单登记、平台回调确认及游戏发货补偿。
type OrderService struct {
    ConfigReader Reader
    Orders interface {
        Find(context.Context, string, string) (model.Order, error)
        Register(context.Context, model.Order) error
        ConfirmPayment(context.Context, int64, string, time.Time) error
        ByID(context.Context, int64) (model.Order, error)
        BeginNotify(context.Context, int64) (bool, error)
        CompleteNotify(context.Context, int64, error) error
        ClaimDue(context.Context) (*model.Order, error)
    }
    GameSecretReader interface {
        LookupGameConfig(context.Context,int64) (json.RawMessage,error)
    }
    PlatformSecretReader interface {
        LookupPlatformSecret(context.Context, string) (string, error)
    }
    Notifier interface {
        ValidateTarget(string) error
        Send(context.Context, model.Order, []byte) error
    }
}

// VerifyRegistration 从 AppID 所属游戏读取主密钥，验证登记请求签名及环境绑定。
func(s *OrderService) VerifyRegistration(r *http.Request,body []byte,env,appid string)error {
    if !validEnv(env)||!orderIDPattern.MatchString(appid){return ErrInvalidOrder}
    app,group,err:=s.ConfigReader.Lookup(r.Context(),appid,env)
    if err!=nil{return err}
    if group==nil{return model.ErrNotFound}
    secret,err:=auth.ParseGameSecret(app.GameConfig)
    if err!=nil{return model.ErrInvalidConfig}
    return auth.VerifyRegistration(r,body,secret)
}

func validEnv(env string)bool {return env=="prod"||env=="test"||env=="review"}

// Register 将游戏订单和大区通知地址存为快照，重复请求必须保持业务参数一致。
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
    app,group,err:=s.ConfigReader.Lookup(ctx,appid,env)
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
        AppID:appid,GameID:app.GameID,Env:env,GameOrderID:req.GameOrderID,
        Amount:req.Amount,Attach:req.Attach,GameNotifyURL:url,
    })
}

// Receive 根据已登记订单定位 AppID，直接读取渠道包共享 appSecret 验签。
// env 仅标识登记订单所属环境，不参与平台验签密钥选择。
// 平台负责核对实际收款；SDK Core 按既有 v1 协议验签并核对登记金额，不发起二次查单。
// 注意：v1 签名不覆盖 amount/attach/paid_at，金额比较无法证明这些回调字段的完整性。
func(s *OrderService) Receive(ctx context.Context,appid string,h http.Header,
    callback model.PaymentCallback)(string,error){
    if !orderIDPattern.MatchString(appid)||!orderIDPattern.MatchString(callback.GameOrderID)||
        !orderIDPattern.MatchString(callback.OrderNo)||callback.Amount<=0 {
        return "",ErrInvalidOrder
    }
    o,err:=s.Orders.Find(ctx,appid,callback.GameOrderID)
    if err!=nil{return "",err}
    secret, err := s.PlatformSecretReader.LookupPlatformSecret(ctx, appid)
    if err != nil { return "", err }
    if err=payment.VerifyV1(h,callback,secret);err!=nil{return "",err}
    if err=validateCallbackDetails(o,callback);err!=nil{return "",err}
    paidAt,err:=time.Parse(time.RFC3339,callback.PaidAt)
    if err!=nil{return "",ErrInvalidOrder}
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

// deliver 使用游戏派生密钥发送发货通知，并独立持久化通知结果。
func(s *OrderService) deliver(ctx context.Context,o model.Order)error {
    sendCtx,cancel:=context.WithTimeout(ctx,5*time.Second)
    defer cancel()
    raw,err:=s.GameSecretReader.LookupGameConfig(sendCtx,o.GameID)
    var key []byte
    if err==nil {
        var secret []byte
        secret,err=auth.ParseGameSecret(raw)
        if err!=nil{err=model.ErrInvalidConfig}else{key,err=auth.Derive(secret,auth.PurposePaymentNotify)}
    }
    if err==nil{err=s.Notifier.Send(sendCtx,o,key)}
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

// Validate 在启动前检查订单服务依赖是否完整。
func(s *OrderService) Validate()error {
    if s.Orders==nil||s.ConfigReader==nil||s.GameSecretReader==nil||s.PlatformSecretReader==nil||s.Notifier==nil {
        return errors.New("order service dependency missing")
    }
     return nil
}



// validateCallbackDetails 只核对回调与已登记的业务字段一致性。
// v1 签名没有覆盖 amount/attach，不能由此证明回调字段未被篡改。
func validateCallbackDetails(order model.Order, callback model.PaymentCallback) error {
    if callback.Amount != order.Amount || callback.Attach != order.Attach {
        return model.ErrOrderConflict
    }
    return nil
}
