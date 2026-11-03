package auth

import (
    "bytes"
    "crypto/md5"
    "crypto/subtle"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "sort"
    "strconv"
    "strings"
    "time"
)

const (
    OperationOrderRegister = "order.register"
    OperationPaymentNotify = "payment.notify"
)

// Sign 与 Java sortJoinSign + gameSecret + MD5 大写算法严格一致。
// 仅接收 JSON 可表达的标量参数；对象和数组应由调用方预先转为约定的字符串。
func Sign(data map[string]any,secret []byte)(string,error){
    if len(secret)==0{return "",ErrGameSecret}
    keys:=make([]string,0,len(data))
    for key:=range data{keys=append(keys,key)}
    sort.Strings(keys)
    parts:=make([]string,0,len(keys))
    for _,key:=range keys{
        if key=="sign"{continue}
        value:=data[key]
        if value==nil{continue}
        var s string
        switch v:=value.(type){
        case string:
            if v==""{continue}
            s=strings.TrimSpace(v)
        case json.Number:
            s=strings.TrimSpace(v.String())
        case bool,int,int8,int16,int32,int64,uint,uint8,uint16,uint32,uint64,float32,float64:
            s=strings.TrimSpace(fmt.Sprint(v))
        default:
            return "",fmt.Errorf("不支持的签名参数类型: %s",key)
        }
        parts=append(parts,key+"="+s)
    }
    plain:=strings.Join(parts,"&")+string(secret)
    sum:=md5.Sum([]byte(plain))
    return strings.ToUpper(hex.EncodeToString(sum[:])) ,nil
}

// SignRequest 将路由标识、请求头和 JSON Body 的顶层字段放入同一张签名参数表。
func SignRequest(secret []byte,operation,env,appid,timestamp,nonce string,body []byte)(string,error){
    if operation==""||env==""||appid==""||timestamp==""||nonce==""{
        return "",ErrUnauthorized
    }
    fields:=map[string]any{
        "action":operation,"appid":appid,"env":env,
        "timestamp":timestamp,"nonce":nonce,
    }
    decoder:=json.NewDecoder(bytes.NewReader(body))
    decoder.UseNumber()
    var values map[string]any
    if err:=decoder.Decode(&values);err!=nil||values==nil {
        return "",errors.New("签名请求体必须是 JSON 对象")
    }
    var extra any
    if err:=decoder.Decode(&extra);err!=io.EOF{
        return "",errors.New("签名请求体包含多余数据")
    }
    for key,value:=range values{
        if key=="sign"{
            return "",errors.New("请求体不允许使用 sign 字段")
        }
        if _,exists:=fields[key];exists{
            return "",errors.New("请求体字段与签名公共字段冲突")
        }
        fields[key]=value
    }
    return Sign(fields,secret)
}

// VerifyRequest 校验时间窗口、随机数格式、业务归属和签名。
func VerifyRequest(r *http.Request,body,secret []byte,operation,env,appid string)error{
    timestamp:=r.Header.Get("x-timestamp")
    nonce:=r.Header.Get("x-nonce")
    signature:=r.Header.Get("x-signature")
    if r.Header.Get("x-app-id")!=appid||r.Header.Get("x-env")!=env||
        len(nonce)!=16||strings.ToLower(nonce)!=nonce||
        len(signature)!=32||strings.ToUpper(signature)!=signature{
        return ErrUnauthorized
    }
    nonceBytes,err:=hex.DecodeString(nonce)
    if err!=nil||len(nonceBytes)!=8{return ErrUnauthorized}
    provided,err:=hex.DecodeString(signature)
    if err!=nil{return ErrUnauthorized}
    stamp,err:=strconv.ParseInt(timestamp,10,64)
    if err!=nil||time.Since(time.Unix(stamp,0))>5*time.Minute||
        time.Until(time.Unix(stamp,0))>5*time.Minute{return ErrUnauthorized}
    expected,err:=SignRequest(secret,operation,env,appid,timestamp,nonce,body)
    if err!=nil{return ErrUnauthorized}
    calculated,err:=hex.DecodeString(expected)
    if err!=nil||subtle.ConstantTimeCompare(provided,calculated)!=1{return ErrUnauthorized}
    return nil
}

func VerifyRegistration(r *http.Request,body,secret []byte)error{
    if r.Method!=http.MethodPost{return ErrUnauthorized}
    return VerifyRequest(r,body,secret,OperationOrderRegister,r.PathValue("env"),r.PathValue("appid"))
}
