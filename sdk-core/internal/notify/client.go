// Package notify 负责向游戏服务器发送独立签名的发货通知。
package notify

import (
    "bytes"
    "context"
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/netip"
    "net/url"
    "strconv"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

var ErrTarget = errors.New("game notification target not approved")
var denyNetworks = func() []netip.Prefix {
    candidates:=[]string{"100.64.0.0/10","192.0.0.0/24","198.18.0.0/15",
        "192.0.2.0/24","198.51.100.0/24","203.0.113.0/24",
        "2001:db8::/32","2001::/23"}
    result:=make([]netip.Prefix,0,len(candidates))
    for _,s:=range candidates { result=append(result,netip.MustParsePrefix(s)) }
    return result
}()

type Client struct {
    http *http.Client
    allowed map[string]bool
    secrets map[string]string
}

// NewClient 限定目标域名并在 TCP 连接时再次检查 DNS 解析结果。
func NewClient(hosts []string,secrets map[string]string) *Client {
    allow:=make(map[string]bool,len(hosts))
    for _,h:=range hosts {allow[strings.ToLower(h)]=true}
    tr:=&http.Transport{
        DisableKeepAlives:true, TLSHandshakeTimeout:3*time.Second,
        ResponseHeaderTimeout:4*time.Second,
        DialContext: func(ctx context.Context,network,address string)(net.Conn,error){
            host,port,err:=net.SplitHostPort(address)
            if err!=nil{return nil,ErrTarget}
            resolved,err:=net.DefaultResolver.LookupIPAddr(ctx,host)
            if err!=nil||len(resolved)==0{return nil,ErrTarget}
            for _,item:=range resolved {
                ip,ok:=netip.AddrFromSlice(item.IP)
                if !ok || !publicIP(ip.Unmap()) {return nil,ErrTarget}
            }
            return (&net.Dialer{Timeout:3*time.Second}).DialContext(ctx,network,net.JoinHostPort(resolved[0].IP.String(),port))
        },
    }
    h:=&http.Client{Timeout:5*time.Second,Transport:tr,
        CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
    return &Client{http:h,allowed:allow,secrets:secrets}
}

func publicIP(ip netip.Addr)bool {
    if !ip.IsValid()||!ip.IsGlobalUnicast()||ip.IsPrivate()||ip.IsLoopback()||ip.IsLinkLocalUnicast(){return false}
    for _,prefix:=range denyNetworks {if prefix.Contains(ip){return false}}
    return true
}

func (c *Client) ValidateTarget(raw string)error {
    u,err:=url.Parse(raw)
    if err!=nil||u.Scheme!="https"||u.User!=nil||u.Hostname()==""||
        u.Fragment!=""||u.RawQuery!=""||u.Opaque!="" {return ErrTarget}
    host:=strings.ToLower(u.Hostname())
    if !c.allowed[host]||net.ParseIP(host)!=nil{return ErrTarget}
    if u.Port()!="" && u.Port()!="443" {return ErrTarget}
    return nil
}

// Send 使用 SDK Core 与游戏间的 HMAC-SHA256，不转发平台密钥。
// 签名串: appid, POST, path, timestamp, nonce, SHA256(rawBody)，以 LF 连接。
func(c *Client)Send(ctx context.Context,o model.Order)error {
    if err:=c.ValidateTarget(o.GameNotifyURL);err!=nil{return err}
    secret:=c.secrets[o.AppID]
    if len(secret)<32{return errors.New("missing game notification secret")}
    body,err:=json.Marshal(model.PaymentCallback{
        OrderNo:o.PlatformOrderNo,GameOrderID:o.GameOrderID,
        Amount:o.Amount,Attach:o.Attach,PaidAt:o.PaidAt.UTC().Format(time.RFC3339),
    })
    if err!=nil{return err}
    u,_:=url.Parse(o.GameNotifyURL)
    var b [8]byte
    if _,err=rand.Read(b[:]);err!=nil{return err}
    nonce:=hex.EncodeToString(b[:])
    ts:=strconv.FormatInt(time.Now().Unix(),10)
    sum:=sha256.Sum256(body)
    canonical:=fmt.Sprintf("%s\nPOST\n%s\n%s\n%s\n%x",o.AppID,u.EscapedPath(),ts,nonce,sum)
    mac:=hmac.New(sha256.New,[]byte(secret))
    mac.Write([]byte(canonical))
    req,err:=http.NewRequestWithContext(ctx,http.MethodPost,o.GameNotifyURL,bytes.NewReader(body))
    if err!=nil{return err}
    req.Header.Set("Content-Type","application/json")
    req.Header.Set("X-SDK-AppID",o.AppID)
    req.Header.Set("X-SDK-Timestamp",ts)
    req.Header.Set("X-SDK-Nonce",nonce)
    req.Header.Set("X-SDK-Signature",hex.EncodeToString(mac.Sum(nil)))
    resp,err:=c.http.Do(req)
    if err!=nil{return fmt.Errorf("notification transport error: %w",err)}
    defer resp.Body.Close()
    ack,err:=io.ReadAll(io.LimitReader(resp.Body,65))
    if err!=nil{return errors.New("invalid notification response")}
    if resp.StatusCode!=http.StatusOK||string(ack)!=nonce {
        return fmt.Errorf("notification not acknowledged: status=%d",resp.StatusCode)
    }
    return nil
}
