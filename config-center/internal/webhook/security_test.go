package webhook

import (
    "context"
    "net"
    "net/netip"
    "strings"
    "testing"
)

func TestWebhookIPPolicy(t *testing.T) {
    tests := []struct {
        ip string
        allowed bool
    }{
        {"8.8.8.8", true},
        {"10.0.0.5", true},
        {"192.168.1.10", true},
        {"127.0.0.1", true},
        {"::1", true},
        {"100.64.0.1", true},
        {"fd00::1", true},
        {"169.254.169.254", false},
        {"100.100.100.200", false},
        {"fd00:ec2::254", false},
        {"224.0.0.1", false},
        {"fe80::1", false},
    }
    for _, tt := range tests {
        if got := permittedWebhookIP(netip.MustParseAddr(tt.ip)); got != tt.allowed {
            t.Errorf("IP %s: got %t, want %t", tt.ip, got, tt.allowed)
        }
    }
}

func TestCallbackURLSupportsPublicAndPrivateTargets(t *testing.T) {
    for _, raw := range []string{
        "https://example.com/notify",
        "http://8.8.8.8:8080/notify",
        "http://192.168.1.10:9000/notify",
        "http://127.0.0.1:8081/notify",
        "http://localhost:8081/notify",
        "https://[::1]:8443/notify",
    } {
        if err := ValidateCallback(raw); err != nil {
            t.Errorf("should allow %s: %v", raw, err)
        }
    }
    for _, raw := range []string{
        "file:///etc/passwd",
        "http://user:pass@localhost/notify",
        "http://169.254.169.254/latest/meta-data/",
        "http://100.100.100.200/notify",
        "https://metadata.google.internal/latest/",
        "http://example.com:99999/notify",
    } {
        if err := ValidateCallback(raw); err == nil {
            t.Errorf("should reject %s", raw)
        }
    }
}

func TestWebhookDialBlocksMetadataDNSAlias(t *testing.T) {
    resolver := func(context.Context, string) ([]net.IPAddr, error) {
        return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
    }
    dial := secureWebhookDial(resolver)
    _, err := dial(context.Background(), "tcp", "callback.example.org:8080")
    if err == nil || !strings.Contains(err.Error(), "prohibited") {
        t.Fatalf("expected metadata rejection, got %v", err)
    }
}
