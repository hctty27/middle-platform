package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/shared/signature"
)

func TestReceivePaidAtFormats(t *testing.T) {
	cases := []struct {
		name    string
		paidAt  string
		want    string
		invalid bool
	}{
		{name: "sender milliseconds", paidAt: "2026-11-13 12:09:23.524+08", want: "2026-11-13T04:09:23.524Z"},
		{name: "sender seconds", paidAt: "2026-11-13 12:09:23+08", want: "2026-11-13T04:09:23Z"},
		{name: "sender negative offset", paidAt: "2026-11-13 12:09:23.524-05", want: "2026-11-13T17:09:23.524Z"},
		{name: "RFC3339 milliseconds", paidAt: "2026-11-13T12:09:23.524+08:00", want: "2026-11-13T04:09:23.524Z"},
		{name: "RFC3339 UTC", paidAt: "2026-11-13T04:09:23Z", want: "2026-11-13T04:09:23Z"},
		{name: "empty", invalid: true},
		{name: "missing timezone", paidAt: "2026-11-13 12:09:23.524", invalid: true},
		{name: "invalid date", paidAt: "2026-11-31 12:09:23.524+08", invalid: true},
		{name: "invalid text", paidAt: "not-a-time", invalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orders := &asyncOrderRepo{order: model.Order{
				ID: 17, AppID: "110005", GameOrderID: "261113120850yapLamWy",
				Amount: 100, Attach: `{"serverId":"99998"}`,
			}}
			svc := &OrderService{Orders: orders, PlatformSecretReader: asyncPlatformSecret{}}
			cb := model.PaymentCallback{
				OrderNo: "261113120901G454P5AJ1LXT", GameOrderID: orders.order.GameOrderID,
				Amount: 100, Attach: orders.order.Attach, PaidAt: tc.paidAt,
			}
			ts := strconv.FormatInt(time.Now().Unix(), 10)
			nonce := "ddbc9abb587b90a5"
			header := http.Header{}
			header.Set("x-timestamp", ts)
			header.Set("x-nonce", nonce)
			header.Set("x-signature", signature.SignAllValues(cb.OrderNo, cb.GameOrderID, "test-platform-secret", nonce, ts))

			ack, err := svc.Receive(context.Background(), "110005", header, cb)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidOrder) {
					t.Fatalf("expected invalid order, got %v", err)
				}
				if orders.confirmations != 0 || orders.order.PaymentStatus != 0 {
					t.Fatal("invalid paid_at confirmed payment")
				}
				return
			}
			if err != nil || ack != nonce {
				t.Fatalf("Receive returned acknowledgment %q, error %v", ack, err)
			}
			want, err := time.Parse(time.RFC3339, tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if !orders.order.PaidAt.Equal(want) || orders.confirmations != 1 {
				t.Fatalf("paid_at = %s, want %s; confirmations = %d", orders.order.PaidAt, want, orders.confirmations)
			}
		})
	}
}
