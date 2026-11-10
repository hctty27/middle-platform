package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

func TestSendUsesHMACSHA256Signature(t *testing.T) {
	handled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Webhook 方法错误: %s", r.Method)
		}
		if r.Header.Get("X-Config-Timestamp") != "" || r.Header.Get("X-Config-Signature") != "" {
			t.Error("仍发送旧版 HMAC Header")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取 Webhook Body 失败: %v", err)
		}
		if !signature.VerifyHMACRequest(r, "webhook-test-secret", body, time.Now()) {
			t.Error("Webhook HMAC-SHA256 签名验证失败")
		}
		if string(body) != `{"epoch":"epoch-1","revision":42}` {
			t.Errorf("Webhook 内容错误: %s", body)
		}
		handled = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	dispatcher := &Dispatcher{Client: server.Client()}
	err := dispatcher.send(context.Background(), Delivery{
		URL: server.URL, Secret: "webhook-test-secret", Epoch: "epoch-1", Revision: 42,
	})
	if err != nil {
		t.Fatalf("Webhook 发送失败: %v", err)
	}
	if !handled {
		t.Fatal("Webhook 未被接收")
	}
}

func TestSendRejectsEmptyWebhookSecret(t *testing.T) {
	dispatcher := &Dispatcher{Client: http.DefaultClient}
	if err := dispatcher.send(context.Background(), Delivery{URL: "http://localhost:8081/notify"}); err == nil {
		t.Fatal("空 Secret 不应发送回调")
	}
}
