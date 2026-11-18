package model

import (
	"errors"
	"time"
)

var (
	ErrOrderNotFound = errors.New("订单不存在")
	ErrOrderConflict = errors.New("订单冲突")
	ErrOrderNotPaid  = errors.New("订单尚未支付")
	// ErrNotifyDeferred 表示本地目标配额饱和，不是游戏服发货失败。
	ErrNotifyDeferred = errors.New("发货通知目标繁忙，本地延期")
)

// RegisterOrder 是游戏服提交的订单登记内容，金额单位为分。
type RegisterOrder struct {
	GameOrderID string `json:"game_order_id"`
	Amount      int64  `json:"amount"`
	Attach      string `json:"attach"`
}

// Order 是 game_order 表的订单快照，支付与通知状态独立保存。
type Order struct {
	ID              int64
	AppID           string
	GameID          int64
	Env             string
	GameOrderID     string
	Amount          int64
	Attach          string
	GameNotifyURL   string
	PlatformOrderNo string
	PaidAt          time.Time
	PaymentStatus   uint8
	NotifyStatus    uint8
	NotifyAttempts  int
	NextNotifyAt    time.Time
}

// NotifyCandidate 仅用于调度预览，不表示已取得订单租约。
type NotifyCandidate struct {
	ID            int64
	GameNotifyURL string
	NextNotifyAt  time.Time
	// DueBefore 固定本轮扫描的数据库时间，持续新订单不能阻止游标回到忙目标。
	DueBefore time.Time
}

// PaymentCallback 表示平台发来的 v1 支付回调。发给游戏的通知不使用此结构。
type PaymentCallback struct {
	OrderNo     string `json:"order_no"`
	GameOrderID string `json:"game_order_id"`
	Amount      int64  `json:"amount"`
	Attach      string `json:"attach"`
	PaidAt      string `json:"paid_at"`
}
