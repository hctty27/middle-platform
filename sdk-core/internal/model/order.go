package model

import (
    "errors"
    "time"
)

var (
    ErrOrderNotFound = errors.New("order not found")
    ErrOrderConflict = errors.New("order conflict")
    ErrOrderNotPaid = errors.New("order not paid")
)

type RegisterOrder struct {
    GameOrderID string `json:"game_order_id"`
    Amount int64 `json:"amount"`
    Attach string `json:"attach"`
}

type Order struct {
    ID int64
    AppID string
    Env string
    GameOrderID string
    Amount int64
    Attach string
    GameNotifyURL string
    PlatformOrderNo string
    PaidAt time.Time
    PaymentStatus uint8
    NotifyStatus uint8
    NotifyAttempts int
    NextNotifyAt time.Time
}

type PaymentCallback struct {
    OrderNo string `json:"order_no"`
    GameOrderID string `json:"game_order_id"`
    Amount int64 `json:"amount"`
    Attach string `json:"attach"`
    PaidAt string `json:"paid_at"`
}
