package notify

import (
    "bytes"
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "io"
    "net"
    "net/http"
    "net/http/httptest"
    "net/netip"
    "testing"
    "time"
    "sort"
    "strings"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

func TestNotificationTargets(t *testing.T) {
    client := NewClient()
    for _, raw := range []string{
        "https://game.example.com/notify",
        "http://8.8.8.8:8080/notify",
        "http://192.168.1.10:9000/notify",
        "http://10.0.0.5:8221/notify",
        "http://127.0.0.1:8080/notify",
        "http://localhost:8080/notify",
        "https://[::1]:8443/notify",
    } {
        if err := client.ValidateTarget(raw); err != nil {
            t.Errorf("should allow %s: %v", raw, err)
        }
    }
    for _, raw := range []string{
        "file:///etc/passwd",
        "https://user:pass@game.example.com/notify",
        "http://169.254.169.254/latest/meta-data/",
        "http://100.100.100.200/notify",
        "http://metadata.google.internal/notify",
        "http://game.example.com:99999/notify",
        "http://game.example.com/notify?token=x",
    } {
        if err := client.ValidateTarget(raw); err == nil {
            t.Errorf("should reject %s", raw)
        }
    }
}

func TestNotificationIPPolicy(t *testing.T) {
    tests := []struct {
        ip string
        allowed bool
    }{
        {"8.8.8.8", true},
        {"10.0.0.5", true},
        {"127.0.0.1", true},
        {"100.64.0.1", true},
        {"fd00::1", true},
        {"169.254.169.254", false},
        {"100.100.100.200", false},
        {"fd00:ec2::254", false},
        {"224.0.0.1", false},
    }
    for _, tt := range tests {
        if got := permittedCallbackIP(netip.MustParseAddr(tt.ip)); got != tt.allowed {
            t.Errorf("IP %s: got %t, want %t", tt.ip, got, tt.allowed)
        }
    }
}

func TestNotificationDialRejectsMetadata(t *testing.T) {
    client := NewClient()
    transport := client.http.Transport.(*http.Transport)
    conn, err := transport.DialContext(context.Background(), "tcp", net.JoinHostPort("169.254.169.254", "80"))
    if conn != nil {
        conn.Close()
        t.Fatal("unexpected metadata connection")
    }
    if err == nil {
        t.Fatal("metadata target must be blocked")
    }
}

func TestNotifySignatureV3DoesNotDependOnURLPath(t *testing.T) {
    key:=bytes.Repeat([]byte{0x5a},32)
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if r.URL.Path!="/arbitrary/game/notify" {t.Errorf("unexpected URL %q",r.URL.Path)}
        body,err:=io.ReadAll(r.Body)
        if err!=nil {t.Errorf("read request: %v",err);w.WriteHeader(http.StatusBadRequest);return}
        timestamp:=r.Header.Get("x-timestamp")
        nonce:=r.Header.Get("x-nonce")
        appid:=r.Header.Get("x-app-id")
        sum:=sha256.Sum256(body)
        selected:=[]string{appid,r.Header.Get("x-env"),"POST","payment.notify",timestamp,nonce,hex.EncodeToString(sum[:]),"P100"}
        sort.Strings(selected)
        canonical:=strings.Join(selected,"_")
        mac:=hmac.New(sha256.New,key)
        _,_=mac.Write([]byte(canonical))
        if got:=r.Header.Get("x-signature");got!=hex.EncodeToString(mac.Sum(nil)) {
            t.Errorf("notification signature unexpectedly includes URL path")
            w.WriteHeader(http.StatusUnauthorized)
            return
        }
        _,_=w.Write([]byte(nonce))
    }))
    defer server.Close()
    order:=model.Order{
        AppID:"110006",Env:"test",GameOrderID:"G100",PlatformOrderNo:"P100",Amount:600,
        GameNotifyURL:server.URL+"/arbitrary/game/notify",PaidAt:time.Now(),
    }
    if err:=NewClient().Send(context.Background(),order,key);err!=nil {
        t.Fatalf("send notification: %v",err)
    }
}
