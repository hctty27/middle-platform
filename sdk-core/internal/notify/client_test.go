package notify

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/shared/signature"
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
		ip      string
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

func TestNotifySignatureUsesHMAC(t *testing.T) {
	secret := []byte("game-secret")
	paidAt := time.Date(2026, 11, 3, 0, 0, 0, 400_000_000, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/arbitrary/game/notify" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var message struct {
			OrderNo     string `json:"order_no"`
			GameOrderID string `json:"game_order_id"`
			Amount      int64  `json:"amount"`
			Attach      string `json:"attach"`
			PaidAt      int64  `json:"paid_at"`
		}
		if err := json.Unmarshal(body, &message); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("x-app-id") != "110006" {
			t.Error("wrong appid")
		}
		if r.Header.Get("X-SDK-AppID") != "" {
			t.Error("obsolete header")
		}
		if message.Amount != 600 || message.GameOrderID != "G100" || message.PaidAt != paidAt.Unix() {
			t.Errorf("bad body: %+v", message)
		}
		if !signature.VerifyGameHMAC(r, secret, "110006", body, time.Now()) {
			t.Error("invalid game notification signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(r.Header.Get("x-nonce")))
	}))
	defer server.Close()
	order := model.Order{
		AppID: "110006", Env: "test", GameOrderID: "G100", PlatformOrderNo: "P100",
		Amount: 600, GameNotifyURL: server.URL + "/arbitrary/game/notify", PaidAt: paidAt,
	}
	if err := NewClient().Send(context.Background(), order, secret); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyRejectsNonPositivePaidAt(t *testing.T) {
	order := model.Order{
		AppID: "110006", GameOrderID: "G100", PlatformOrderNo: "P100",
		Amount: 600, GameNotifyURL: "http://127.0.0.1:8080/notify",
	}
	if err := NewClient().Send(context.Background(), order, []byte("game-secret")); err == nil {
		t.Fatal("zero paid_at must be rejected")
	}
}
