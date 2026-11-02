package service

import (
    "errors"
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

func TestCallbackDetailsMatchRegistration(t *testing.T) {
    order := model.Order{Amount: 600, Attach: "purchase"}
    cases := []struct {
        name string
        callback model.PaymentCallback
        wantError bool
    }{
        {"match", model.PaymentCallback{Amount: 600, Attach: "purchase"}, false},
        {"amount mismatch", model.PaymentCallback{Amount: 100, Attach: "purchase"}, true},
        {"attach mismatch", model.PaymentCallback{Amount: 600, Attach: "other"}, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            err := validateCallbackDetails(order, tc.callback)
            if tc.wantError && !errors.Is(err, model.ErrOrderConflict) {
                t.Fatalf("expected order conflict: %v", err)
            }
            if !tc.wantError && err != nil {
                t.Fatalf("matching registration rejected: %v", err)
            }
        })
    }
}
