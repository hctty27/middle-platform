package api

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
    "github.com/hctty27/middle-platform/sdk-core/internal/payment"
    "github.com/hctty27/middle-platform/sdk-core/internal/service"
)

// OrderAPI 定义订单登记鉴权、入库和平台回调处理所需的业务能力。
type OrderAPI interface {
    VerifyRegistration(*http.Request,[]byte,string,string)error
    Register(context.Context,string,string,model.RegisterOrder)error
    Receive(context.Context,string,http.Header,model.PaymentCallback)(string,error)
}

// decodeOrderJSON 严格拒绝顶层重复键、未知字段、缺字段和非 JSON 对象。
func decodeOrderJSON(raw []byte,required []string,target any)error{
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    start,err:=decoder.Token()
    if err!=nil||start!=json.Delim('{'){return errors.New("请求体必须是 JSON 对象")}
    values:=make(map[string]json.RawMessage)
    allowed:=make(map[string]bool,len(required))
    for _,k:=range required{allowed[k]=true}
    for decoder.More(){
        token,err:=decoder.Token()
        if err!=nil{return err}
        key,ok:=token.(string)
        if !ok||!allowed[key]||values[key]!=nil{return errors.New("请求体包含重复或不支持的字段")}
        var item json.RawMessage
        if err=decoder.Decode(&item);err!=nil{return err}
        if bytes.Equal(bytes.TrimSpace(item),[]byte("null")){return errors.New("请求字段不能为 null")}
        values[key]=item
    }
    if _,err=decoder.Token();err!=nil{return err}
    if err=decoder.Decode(new(any));!errors.Is(err,io.EOF){return errors.New("请求体存在多余 JSON 内容")}
    for _,k:=range required{if values[k]==nil{return errors.New("缺少必填字段")}}
    d:=json.NewDecoder(bytes.NewReader(raw))
    d.DisallowUnknownFields()
    return d.Decode(target)
}

func readBody(w http.ResponseWriter,r *http.Request)([]byte,error){
    r.Body=http.MaxBytesReader(w,r.Body,64<<10)
    return io.ReadAll(r.Body)
}

func orderError(w http.ResponseWriter,err error){
    code:=http.StatusServiceUnavailable
    message:="订单服务暂时不可用"
    switch {
    case errors.Is(err,model.ErrOrderNotFound),errors.Is(err,model.ErrNotFound):
        code=http.StatusNotFound;message="订单或渠道包不存在"
    case errors.Is(err,model.ErrOrderConflict):
        code=http.StatusConflict;message="订单数据冲突"
    case errors.Is(err,service.ErrInvalidOrder):
        code=http.StatusBadRequest;message="订单请求参数不合法"
    case errors.Is(err,auth.ErrUnauthorized),errors.Is(err,payment.ErrSignature):
        code=http.StatusUnauthorized;message="请求签名验证失败"
    case errors.Is(err,model.ErrInvalidConfig):
        code=http.StatusUnprocessableEntity;message="服务器配置不合法"
    }
    writeJSON(w,code,response{Code:code,Message:message})
}

func registerOrderRoutes(mux *http.ServeMux,svc OrderAPI){
    mux.HandleFunc("POST /api/core/{env}/{appid}/orders",func(w http.ResponseWriter,r *http.Request){
        if r.URL.RawQuery!=""{orderError(w,service.ErrInvalidOrder);return}
        raw,err:=readBody(w,r)
        if err!=nil{writeJSON(w,http.StatusRequestEntityTooLarge,response{Code:413,Message:"请求体格式不合法或超过大小限制"});return}
        env,appid:=r.PathValue("env"),r.PathValue("appid")
        if err=svc.VerifyRegistration(r,raw,env,appid);err!=nil{orderError(w,err);return}
        var req model.RegisterOrder
        if err=decodeOrderJSON(raw,[]string{"game_order_id","amount","attach"},&req);err!=nil {
            orderError(w,service.ErrInvalidOrder);return
        }
        if err=svc.Register(r.Context(),env,appid,req);err!=nil{orderError(w,err);return}
        writeJSON(w,http.StatusOK,response{Code:0,Message:"success",
            Data:map[string]string{"game_order_id":req.GameOrderID,"registration_status":"REGISTERED"}})
    })
    mux.HandleFunc("POST /api/core/{appid}/pay/notify",func(w http.ResponseWriter,r *http.Request){
        if r.URL.RawQuery!=""{orderError(w,service.ErrInvalidOrder);return}
        raw,err:=readBody(w,r)
        if err!=nil{writeJSON(w,http.StatusRequestEntityTooLarge,response{Code:413,Message:"请求体格式不合法或超过大小限制"});return}
        var req model.PaymentCallback
        if err=decodeOrderJSON(raw,
            []string{"order_no","game_order_id","amount","attach","paid_at"},&req);err!=nil {
            orderError(w,service.ErrInvalidOrder);return
        }
        nonce,err:=svc.Receive(r.Context(),r.PathValue("appid"),r.Header,req)
        if err!=nil{orderError(w,err);return}
        w.Header().Set("Content-Type","text/plain; charset=utf-8")
        w.WriteHeader(http.StatusOK)
        _,_=w.Write([]byte(nonce))
    })
}
