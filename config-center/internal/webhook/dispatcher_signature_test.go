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

const (
	testWebhookSubscriberCode = "access-test"
	testWebhookSigningSecret = "webhook-test-secret-0123456789abcdef0123456789"
)

func TestSendUsesSubscriberScopedHMACSHA256Signature(t *testing.T) {
	handled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Webhook 方法错误: %s", r.Method)
		}
		if r.Header.Get("X-Subscriber-Code") != testWebhookSubscriberCode {
			t.Errorf("Webhook 接入端编码错误: %q", r.Header.Get("X-Subscriber-Code"))
		}
		if r.Header.Get("X-Config-Timestamp") != "" || r.Header.Get("X-Config-Signature") != "" {
			t.Error("仍发送旧版 HMAC Header")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取 Webhook Body 失败: %v", err)
		}
		if !signature.VerifySubscriberHMACRequest(r, testWebhookSigningSecret, body, time.Now()) {
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
		Code: testWebhookSubscriberCode, URL: server.URL, Secret: testWebhookSigningSecret, Epoch: "epoch-1", Revision: 42,
	})
	if err != nil {
		t.Fatalf("Webhook 发送失败: %v", err)
	}
	if !handled {
		t.Fatal("Webhook 未被接收")
	}
}

func TestSendRejectsMissingSubscriberCredentials(t *testing.T) {
	dispatcher := &Dispatcher{Client: http.DefaultClient}
	if err := dispatcher.send(context.Background(), Delivery{URL: "http://localhost:8081/notify"}); err == nil {
		t.Fatal("缺少接入端身份或签名密钥不应发送回调")
	}
}
