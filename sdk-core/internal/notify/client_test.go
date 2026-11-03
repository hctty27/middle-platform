package notify

import (
    "context"
    "net"
    "net/http"
    "net/netip"
    "testing"
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
