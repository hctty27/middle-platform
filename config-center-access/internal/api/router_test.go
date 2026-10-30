package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTrigger struct { count atomic.Int64 }
func (f *fakeTrigger) Request() { f.count.Add(1) }

// TestCallbackSignatureAndAcceptance 验证合法签名回调触发同步任务且错误签名被拒绝。
func TestCallbackSignatureAndAcceptance(t *testing.T) {
	trigger := &fakeTrigger{}
	handler := Router(nil, trigger, "test-secret")
	body := []byte(`{"epoch":"an-epoch","revision":42}`)
	timestamp := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte(timestamp + ".an-epoch.42"))
	valid := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	for _, test := range []struct {
		name, signature, timestamp string
		want int
	}{
		{"valid",valid,timestamp,202},
		{"invalid","sha256=bad",timestamp,401},
		{"old",valid,fmt.Sprint(time.Now().Add(-10*time.Minute).Unix()),401},
		{"missing","",timestamp,401},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost,"/internal/config/callback",bytes.NewReader(body))
			req.Header.Set("X-Config-Timestamp",test.timestamp)
			req.Header.Set("X-Config-Signature",test.signature)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != test.want { t.Fatalf("got %d, want %d",w.Code,test.want) }
		})
	}
	if got := trigger.count.Load(); got != 1 { t.Fatalf("accepted triggers=%d",got) }
}

// TestCallbackRejectsMalformedJSON 确保无效 JSON 无法触发接入侧同步。
func TestCallbackRejectsMalformedJSON(t *testing.T) {
	trigger := &fakeTrigger{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/config/callback", bytes.NewBufferString("{bad"))
	Router(nil,trigger,"secret").ServeHTTP(w,req)
	if w.Code != http.StatusBadRequest || trigger.count.Load()!=0 { t.Fatal(w.Code) }
}
