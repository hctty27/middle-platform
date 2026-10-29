package config

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
)

func TestWebhookTargetPolicy(t *testing.T) {
	for _, input := range []struct{
		host, ip string
		allowed bool
	}{
		{"webhook.example.org", "1.1.1.1", true},
		{"webhook.example.org", "10.0.0.5", false},
		{"webhook.example.org", "127.0.0.1", false},
		{"webhook.example.org", "169.254.169.254", false},
		{"webhook.example.org", "100.64.0.1", false},
		{"webhook.example.org", "198.18.0.1", false},
		{"webhook.example.org", "192.0.2.1", false},
		{"webhook.example.org", "2001:db8::1", false},
		{"webhook.example.org", "::ffff:127.0.0.1", false},
		{"webhook.example.org", "fc00::1", false},
		{"localhost", "127.0.0.1", true},
		{"localhost", "::1", true},
		{"localhost", "10.0.0.1", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"10.1.2.3", "10.1.2.3", true},
		{"10.1.2.3", "10.1.2.4", false},
	} {
		got := permittedWebhookIP(input.host, netip.MustParseAddr(input.ip))
		if got != input.allowed { t.Errorf("%s -> %s: got %t, want %t", input.host, input.ip, got, input.allowed) }
	}
}

func TestWebhookDialBlocksDNSRebinding(t *testing.T) {
	resolver := func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
	}
	dial := secureWebhookDial("callback.example.org", resolver)
	_, err := dial(context.Background(), "tcp", "callback.example.org:443")
	if err == nil || !strings.Contains(err.Error(), "prohibited") {
		t.Fatalf("expected DNS rebinding rejection, got %v", err)
	}
	_, err = dial(context.Background(), "tcp", "other.example.org:443")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected host allowlist rejection, got %v", err)
	}
}
