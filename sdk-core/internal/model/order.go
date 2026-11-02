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

// RegisterOrder 是游戏服提交的订单登记内容，金额单位为分。
type RegisterOrder struct {
    GameOrderID string `json:"game_order_id"`
    Amount int64 `json:"amount"`
    Attach string `json:"attach"`
}

// Order 是 game_order 表的订单快照，支付与通知状态独立保存。
type Order struct {
    ID int64
    AppID string
    GameID int64
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

// PaymentCallback 表示平台 v1 回调数据，也用于发送游戏发货通知。
type PaymentCallback struct {
    OrderNo string `json:"order_no"`
    GameOrderID string `json:"game_order_id"`
    Amount int64 `json:"amount"`
    Attach string `json:"attach"`
    PaidAt string `json:"paid_at"`
}
