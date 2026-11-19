package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
)

type businessLogOrderStore struct {
	found      model.Order
	findErr    error
	registerErr error
	notifyErr  error
}

func (s *businessLogOrderStore) Find(context.Context, string, string) (model.Order, error) {
	return s.found, s.findErr
}
func (s *businessLogOrderStore) Register(context.Context, model.Order) error {
	return s.registerErr
}
func (s *businessLogOrderStore) ConfirmPayment(context.Context, int64, string, time.Time) error {
	return nil
}
func (s *businessLogOrderStore) CompleteNotify(context.Context, int64, int, error) error {
	return s.notifyErr
}
func (s *businessLogOrderStore) ClaimDue(context.Context) (*model.Order, error) {
	return nil, nil
}

type businessLogNotifier struct{}

func (businessLogNotifier) ValidateTarget(string) error { return nil }
func (businessLogNotifier) Send(context.Context, model.Order, []byte) error { return nil }

type businessLogSecretReader struct{}

func (businessLogSecretReader) LookupPlatformSecret(context.Context, string) (string, error) {
	return "test-secret-not-for-logs", nil
}

func captureOrderBusinessLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	out := new(bytes.Buffer)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return out
}

func orderLogRecord(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected a business log")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &record); err != nil {
		t.Fatalf("decode log: %v: %s", err, out.String())
	}
	return record
}

func TestPaymentOrderRegistrationBusinessLogs(t *testing.T) {
	out := captureOrderBusinessLogs(t)
	store := &businessLogOrderStore{findErr: model.ErrOrderNotFound}
	svc := &OrderService{Orders: store, Notifier: businessLogNotifier{}}
	req := model.RegisterOrder{GameOrderID: "game-123", Amount: 600, Attach: "private-attach-do-not-log"}
	verified := VerifiedRegistration{
		AppID: "12345", Env: "prod",
		App: model.ChannelApp{Code: "12345", GameID: 42},
		Group: model.BoundGroup{RegionSDKConfig: json.RawMessage(`{"payNotifyUrl":"https://example.com/game/notify"}`)},
	}
	if err := svc.Register(context.Background(), "prod", "12345", req, verified); err != nil {
		t.Fatal(err)
	}
	record := orderLogRecord(t, out)
	if record["msg"] != "订单登记已处理" || record["app_id"] != "12345" || record["game_order_id"] != "game-123" {
		t.Fatalf("registration log: %v", record)
	}
	if strings.Contains(out.String(), req.Attach) {
		t.Fatalf("sensitive attach was logged: %s", out.String())
	}

	out.Reset()
	store.findErr = nil
	store.found = model.Order{Env: "prod", Amount: req.Amount, Attach: req.Attach}
	if err := svc.Register(context.Background(), "prod", "12345", req, verified); err != nil {
		t.Fatal(err)
	}
	if got := orderLogRecord(t, out)["msg"]; got != "订单重复登记，幂等返回" {
		t.Fatalf("idempotent registration log: %v", got)
	}

	out.Reset()
	store.findErr = model.ErrOrderNotFound
	store.registerErr = model.ErrOrderConflict
	if err := svc.Register(context.Background(), "prod", "12345", req, verified); !errors.Is(err, model.ErrOrderConflict) {
		t.Fatalf("registration conflict: %v", err)
	}
	record = orderLogRecord(t, out)
	if record["level"] != "WARN" || record["failure"] != "order_conflict" || record["stage"] != "persist_order" {
		t.Fatalf("registration failure log: %v", record)
	}
	if strings.Contains(out.String(), req.Attach) {
		t.Fatalf("sensitive attach was logged: %s", out.String())
	}
}

func TestPaymentCallbackFailureLogSafe(t *testing.T) {
	out := captureOrderBusinessLogs(t)
	store := &businessLogOrderStore{
		found: model.Order{ID: 77, AppID: "12345", GameOrderID: "game-123", Amount: 600},
	}
	svc := &OrderService{Orders: store, PlatformSecretReader: businessLogSecretReader{}}
	callback := model.PaymentCallback{
		OrderNo: "platform-987", GameOrderID: "game-123",
		Amount: 600, Attach: "sensitive-callback-attach", PaidAt: "2026-11-19T08:00:00Z",
	}
	if _, err := svc.Receive(context.Background(), "12345", http.Header{}, callback); err == nil {
		t.Fatal("expected signature verification to fail")
	}
	record := orderLogRecord(t, out)
	if record["msg"] != "支付回调处理失败" || record["stage"] != "verify_signature" || record["failure"] != "signature_invalid" ||
		record["order_id"] != float64(77) || record["platform_order_no"] != "platform-987" {
		t.Fatalf("callback failure log: %v", record)
	}
	if strings.Contains(out.String(), callback.Attach) || strings.Contains(out.String(), "test-secret-not-for-logs") {
		t.Fatalf("sensitive payment material was logged: %s", out.String())
	}
}

func TestPaymentNotifyBusinessLogs(t *testing.T) {
	out := captureOrderBusinessLogs(t)
	store := &businessLogOrderStore{}
	svc := &OrderService{Orders: store}
	order := model.Order{
		ID: 77, AppID: "12345", GameOrderID: "game-123", PlatformOrderNo: "platform-987",
		NotifyAttempts: 2, Attach: "private-order-attach",
	}
	if err := svc.completeNotify(order, nil); err != nil {
		t.Fatal(err)
	}
	record := orderLogRecord(t, out)
	if record["msg"] != "游戏发货通知完成" || record["attempt"] != float64(2) {
		t.Fatalf("notify success log: %v", record)
	}
	out.Reset()
	remoteErr := errors.New("sensitive-remote-response")
	if err := svc.completeNotify(order, remoteErr); !errors.Is(err, remoteErr) {
		t.Fatalf("notify failure: %v", err)
	}
	record = orderLogRecord(t, out)
	if record["level"] != "WARN" || record["failure"] != "notify_failed" {
		t.Fatalf("notify failure log: %v", record)
	}
	if strings.Contains(out.String(), "sensitive-remote-response") || strings.Contains(out.String(), order.Attach) {
		t.Fatalf("sensitive notification content was logged: %s", out.String())
	}
	out.Reset()
	if err := svc.completeNotify(order, model.ErrNotifyDeferred); !errors.Is(err, model.ErrNotifyDeferred) {
		t.Fatalf("deferred notify: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("local gate deferral should not emit an INFO/WARN log: %s", out.String())
	}
	out.Reset()
	store.notifyErr = model.ErrNotifyResultNotApplied
	if err := svc.completeNotify(order, nil); !errors.Is(err, model.ErrNotifyResultNotApplied) {
		t.Fatalf("expected stale completion error, got %v", err)
	}
	record = orderLogRecord(t, out)
	if record["level"] != "WARN" || record["msg"] != "发货通知结果未写入" || record["failure"] != "stale_or_completed" {
		t.Fatalf("stale completion log: %v", record)
	}
	out.Reset()
	store.notifyErr = errors.New("sensitive-database-error")
	if err := svc.completeNotify(order, nil); err == nil {
		t.Fatal("expected persistence error")
	}
	record = orderLogRecord(t, out)
	if record["level"] != "ERROR" || record["failure"] != "persist_failed" {
		t.Fatalf("persist failure log: %v", record)
	}
	if strings.Contains(out.String(), "sensitive-database-error") {
		t.Fatalf("sensitive persistence error was logged: %s", out.String())
	}
}
