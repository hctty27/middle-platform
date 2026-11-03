// Package auth 实现游戏侧 V3 签名；第三方平台原生 V1 由 payment 包处理。
package auth

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "embed"
    "encoding/hex"
    "encoding/json"
    "errors"
    "net/http"
    "sort"
    "strconv"
    "strings"
    "time"
)

//go:embed signature-rules.json
var rulesFile embed.FS

const (
    OperationOrderRegister = "order.register"
    OperationPaymentNotify = "payment.notify"
)

// Message 表示签名数据，URL 路径或反向代理前缀不参与签名。
type Message struct {
    Operation string
    Method string
    Env string
    AppID string
    Timestamp string
    Nonce string
    Body []byte
}

type signatureRule struct {
    Algorithm string `json:"algorithm"`
    Fields []string `json:"fields"`
}

var signatureRules = loadRules()

func loadRules() map[string]signatureRule {
    raw,err:=rulesFile.ReadFile("signature-rules.json")
    if err!=nil {panic(err)}
    rules:=make(map[string]signatureRule)
    if err=json.Unmarshal(raw,&rules);err!=nil {panic(err)}
    if len(rules)==0 {panic("签名规则不能为空")}
    supported:=map[string]bool{
        "appid":true,"env":true,"method":true,"operation":true,
        "timestamp":true,"nonce":true,"body.sha256":true,
    }
    required:=[]string{"appid","env","method","operation","timestamp","nonce","body.sha256"}
    for operation,rule:=range rules {
        if rule.Algorithm!="hmac-sha256" {panic("签名算法不合法: "+operation)}
        seen:=make(map[string]bool)
        for _,field:=range rule.Fields {
            if seen[field]||(!supported[field]&&!strings.HasPrefix(field,"body."))||field=="body." {
                panic("重复或不支持的签名字段: "+field)
            }
            seen[field]=true
        }
        for _,field:=range required {
            if !seen[field] {panic("签名规则缺少必要字段: "+operation+"/"+field)}
        }
    }
    return rules
}

// SigningFields 返回接口参与签名的字段，可用于联调和文档校验。
func SigningFields(operation string)([]string,error){
    rule,ok:=signatureRules[operation]
    if !ok {return nil,errors.New("接口尚未配置签名规则")}
    return append([]string(nil),rule.Fields...),nil
}

// Sign 使用共享主密钥，根据消息方向派生独立 HMAC 密钥。
func Sign(master []byte,purpose string,message Message)(string,error){
    key,err:=Derive(master,purpose)
    if err!=nil{return "",err}
    return SignWithKey(key,message)
}

// SignWithKey 按接口规则取值、过滤空字符串、字典序排序，以下划线连接并做 HMAC-SHA256。
func SignWithKey(key []byte,message Message)(string,error){
    if len(key)!=keyLength{return "",ErrGameSecret}
    rule,ok:=signatureRules[message.Operation]
    if !ok{return "",errors.New("接口尚未配置签名规则")}
    if message.Method==""||message.AppID==""||message.Env==""||
        message.Timestamp==""||message.Nonce=="" {return "",ErrUnauthorized}
    bodyHash:=sha256.Sum256(message.Body)
    values:=map[string]string{
        "appid":message.AppID,"env":message.Env,"method":message.Method,
        "operation":message.Operation,"timestamp":message.Timestamp,
        "nonce":message.Nonce,"body.sha256":hex.EncodeToString(bodyHash[:]),
    }
    var bodyFields map[string]json.RawMessage
    for _,field:=range rule.Fields {
        if !strings.HasPrefix(field,"body.")||field=="body.sha256"{continue}
        if bodyFields==nil {
            if err:=json.Unmarshal(message.Body,&bodyFields);err!=nil||bodyFields==nil{
                return "",errors.New("签名请求体必须是 JSON 对象")
            }
        }
        raw,ok:=bodyFields[strings.TrimPrefix(field,"body.")]
        if !ok||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){continue}
        var value string
        if len(raw)>0&&raw[0]=='"' {
            if err:=json.Unmarshal(raw,&value);err!=nil{return "",err}
        }else{
            var compact bytes.Buffer
            if err:=json.Compact(&compact,raw);err!=nil{return "",err}
            value=compact.String()
        }
        values[field]=value
    }
    selected:=make([]string,0,len(rule.Fields))
    for _,field:=range rule.Fields{
        if v:=values[field];v!=""{selected=append(selected,v)}
    }
    sort.Strings(selected)
    mac:=hmac.New(sha256.New,key)
    _,_=mac.Write([]byte(strings.Join(selected,"_")))
    return hex.EncodeToString(mac.Sum(nil)),nil
}

// VerifyRequest 校验平台风格的请求头、时间窗口、业务标识与完整内容。
func VerifyRequest(r *http.Request,rawBody,master []byte,purpose,operation,env,appid string)error {
    timestamp:=r.Header.Get("x-timestamp")
    nonce:=r.Header.Get("x-nonce")
    signature:=r.Header.Get("x-signature")
    if r.Header.Get("x-app-id")!=appid||r.Header.Get("x-env")!=env||
        len(nonce)!=16||strings.ToLower(nonce)!=nonce||
        len(signature)!=64||strings.ToLower(signature)!=signature{return ErrUnauthorized}
    nonceBytes,err:=hex.DecodeString(nonce)
    if err!=nil||len(nonceBytes)!=8{return ErrUnauthorized}
    provided,err:=hex.DecodeString(signature)
    if err!=nil{return ErrUnauthorized}
    stamp,err:=strconv.ParseInt(timestamp,10,64)
    if err!=nil||time.Since(time.Unix(stamp,0))>5*time.Minute||
        time.Until(time.Unix(stamp,0))>5*time.Minute{return ErrUnauthorized}
    expected,err:=Sign(master,purpose,Message{
        Operation:operation,Method:r.Method,Env:env,AppID:appid,
        Timestamp:timestamp,Nonce:nonce,Body:rawBody,
    })
    if err!=nil{return ErrUnauthorized}
    calculated,err:=hex.DecodeString(expected)
    if err!=nil||!hmac.Equal(provided,calculated){return ErrUnauthorized}
    return nil
}

// VerifyRegistration 使用订单登记专属签名规则，环境和 AppID 取自服务端路由。
func VerifyRegistration(r *http.Request,rawBody,master []byte)error{
    if r.Method!=http.MethodPost{return ErrUnauthorized}
    return VerifyRequest(r,rawBody,master,PurposeGameRequest,
        OperationOrderRegister,r.PathValue("env"),r.PathValue("appid"))
}
