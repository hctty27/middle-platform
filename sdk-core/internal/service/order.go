package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/auth"
	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/sdk-core/internal/payment"
)

// ErrInvalidOrder 表示登记或回调的订单参数不符合约束。
var ErrInvalidOrder = errors.New("订单请求参数不合法")
var orderIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// OrderConfigReader 保留订单登记所需的游戏密钥和大区通知地址查询。
type OrderConfigReader interface {
	Lookup(context.Context, string, string) (model.ChannelApp, *model.BoundGroup, error)
}

// VerifiedRegistration 保存同一次请求验签时读取的配置快照，供登记阶段复用。
type VerifiedRegistration struct {
	AppID string
	Env   string
	App   model.ChannelApp
	Group model.BoundGroup
}

// OrderService 负责订单登记、平台回调确认及游戏发货补偿。
type OrderService struct {
	ConfigReader OrderConfigReader
	Orders       interface {
		Find(context.Context, string, string) (model.Order, error)
		Register(context.Context, model.Order) error
		ConfirmPayment(context.Context, int64, string, time.Time) error
		CompleteNotify(context.Context, int64, int, error) error
		ClaimDue(context.Context) (*model.Order, error)
	}
	GameSecretReader interface {
		LookupGameConfig(context.Context, int64) (json.RawMessage, error)
	}
	PlatformSecretReader interface {
		LookupPlatformSecret(context.Context, string) (string, error)
	}
	// HostGate 为可选的每目标非阻塞配额；nil 保持旧行为。
	HostGate interface {
		TryAcquire(string) (func(), bool)
	}
	Notifier interface {
		ValidateTarget(string) error
		Send(context.Context, model.Order, []byte) error
	}
}

// VerifyRegistration 读取一次渠道配置并完成验签，返回当前请求可复用的配置快照。
func (s *OrderService) VerifyRegistration(r *http.Request, body []byte, env, appid string) (VerifiedRegistration, error) {
	if !validEnv(env) || !orderIDPattern.MatchString(appid) {
		return VerifiedRegistration{}, ErrInvalidOrder
	}
	app, group, err := s.ConfigReader.Lookup(r.Context(), appid, env)
	if err != nil {
		return VerifiedRegistration{}, err
	}
	if group == nil {
		return VerifiedRegistration{}, model.ErrNotFound
	}
	secret, err := auth.ParseGameSecret(app.GameSDKConfig)
	if err != nil {
		return VerifiedRegistration{}, model.ErrInvalidConfig
	}
	if err = auth.VerifyRegistration(r, body, secret); err != nil {
		return VerifiedRegistration{}, err
	}
	return VerifiedRegistration{
		AppID: appid,
		Env:   env,
		App:   app,
		Group: *group,
	}, nil
}

func validEnv(env string) bool { return env == "prod" || env == "test" || env == "review" }

// Register 将游戏订单和大区通知地址存为快照，重复请求必须保持业务参数一致。
func (s *OrderService) Register(ctx context.Context, env, appid string, req model.RegisterOrder, verified VerifiedRegistration) error {
	if !validEnv(env) || !orderIDPattern.MatchString(appid) ||
		!orderIDPattern.MatchString(req.GameOrderID) || req.Amount <= 0 ||
		req.Amount > 9007199254740991 || len(req.Attach) > 4096 ||
		verified.AppID != appid || verified.Env != env ||
		verified.App.Code != appid || verified.App.GameID <= 0 {
		return ErrInvalidOrder
	}
	// 已存在订单的重试不依赖当前大区通知地址，避免配置变更破坏幂等。
	previous, lookupErr := s.Orders.Find(ctx, appid, req.GameOrderID)
	if lookupErr == nil {
		if previous.Env != env || previous.Amount != req.Amount || previous.Attach != req.Attach {
			return model.ErrOrderConflict
		}
		return nil
	}
	if !errors.Is(lookupErr, model.ErrOrderNotFound) {
		return lookupErr
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(verified.Group.RegionSDKConfig, &config); err != nil {
		return model.ErrInvalidConfig
	}
	raw, ok := config["payNotifyUrl"]
	if !ok {
		return model.ErrInvalidConfig
	}
	var url string
	if err := json.Unmarshal(raw, &url); err != nil || url == "" {
		return model.ErrInvalidConfig
	}
	if err := s.Notifier.ValidateTarget(url); err != nil {
		return model.ErrInvalidConfig
	}
	return s.Orders.Register(ctx, model.Order{
		AppID: appid, GameID: verified.App.GameID, Env: env, GameOrderID: req.GameOrderID,
		Amount: req.Amount, Attach: req.Attach, GameNotifyURL: url,
	})
}

// Receive 根据已登记订单定位 AppID，验签后仅确认支付并唤醒持久化通知队列。
// env 仅标识登记订单所属环境，不参与平台验签密钥选择。
// 平台负责核对实际收款；SDK Core 按既有 v1 协议验签并核对登记金额，不发起二次查单。
// 注意：v1 签名不覆盖 amount/attach/paid_at，金额比较无法证明这些回调字段的完整性。
func (s *OrderService) Receive(ctx context.Context, appid string, h http.Header,
	callback model.PaymentCallback) (string, error) {
	if !orderIDPattern.MatchString(appid) || !orderIDPattern.MatchString(callback.GameOrderID) ||
		!orderIDPattern.MatchString(callback.OrderNo) || callback.Amount <= 0 {
		return "", ErrInvalidOrder
	}
	o, err := s.Orders.Find(ctx, appid, callback.GameOrderID)
	if err != nil {
		return "", err
	}
	secret, err := s.PlatformSecretReader.LookupPlatformSecret(ctx, appid)
	if err != nil {
		return "", err
	}
	if err = payment.VerifyV1(h, callback, secret); err != nil {
		return "", err
	}
	if err = validateCallbackDetails(o, callback); err != nil {
		return "", err
	}
	paidAt, err := time.Parse(time.RFC3339, callback.PaidAt)
	if err != nil {
		paidAt, err = time.Parse("2006-01-02 15:04:05Z07", callback.PaidAt)
	}
	if err != nil {
		return "", ErrInvalidOrder
	}
	paidAt = paidAt.UTC().Truncate(time.Second)
	if err = s.Orders.ConfirmPayment(ctx, o.ID, callback.OrderNo, paidAt); err != nil {
		return "", err
	}
	// 支付状态已持久化，立即向平台确认；游戏发货由持久化 Worker 异步处理。
	// 并发重复回调只会重复确认同一平台订单，不会触发额外网络通知。
	return h.Get("x-nonce"), nil
}

// deliver 使用游戏主密钥按平台风格签名通知，并持久化通知结果。
func (s *OrderService) deliver(ctx context.Context, o model.Order) error {
	// 额度满时不读密钥、不发送、不等待；网络阶段结束后即释放，保存不占额度。
	err := func() error {
		if s.HostGate != nil {
			release, ok := s.HostGate.TryAcquire(o.GameNotifyURL)
			if !ok {
				return model.ErrNotifyDeferred
			}
			defer release()
		}
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		raw, err := s.GameSecretReader.LookupGameConfig(sendCtx, o.GameID)
		var key []byte
		if err == nil {
			key, err = auth.ParseGameSecret(raw)
			if err != nil {
				err = model.ErrInvalidConfig
			}
		}
		if err == nil {
			err = s.Notifier.Send(sendCtx, o, key)
		}
		return err
	}()
	commitCtx, commitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer commitCancel()
	if saveErr := s.Orders.CompleteNotify(commitCtx, o.ID, o.NotifyAttempts, err); saveErr != nil {
		return fmt.Errorf("保存发货通知结果失败: %w", saveErr)
	}
	return err
}

// RunWorker 使用可配置数量的消费者处理到期通知。
// 通过 MySQL SKIP LOCKED 协调多进程；60 秒租约长于 5 秒通知超时。
func (s *OrderService) RunWorker(ctx context.Context, workers int, idleInterval time.Duration) {
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			s.runNotifyWorker(ctx, idleInterval)
		}()
	}
	wg.Wait()
}

// runNotifyWorker 在存在积压时连续领取订单，仅空闲或领取失败时等待。
func (s *OrderService) runNotifyWorker(ctx context.Context, idleInterval time.Duration) {
	var lastClaimErrorLog time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		jobCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		order, err := s.Orders.ClaimDue(jobCtx)
		cancel()
		if err == nil && order != nil {
			if notifyErr := s.deliver(ctx, *order); notifyErr != nil && !errors.Is(notifyErr, model.ErrNotifyDeferred) && ctx.Err() == nil {
				slog.Warn("游戏发货通知失败", "order_id", order.ID,
					"attempt", order.NotifyAttempts, "error", notifyErr)
			}
			continue
		}
		if err != nil && ctx.Err() == nil && time.Since(lastClaimErrorLog) >= 30*time.Second {
			slog.Warn("领取游戏发货通知任务失败", "error", err)
			lastClaimErrorLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(idleInterval):
		}
	}
}

// Validate 在启动前检查订单服务依赖是否完整。
func (s *OrderService) Validate() error {
	if s.Orders == nil || s.ConfigReader == nil || s.GameSecretReader == nil || s.PlatformSecretReader == nil || s.Notifier == nil {
		return errors.New("订单服务依赖未配置完整")
	}
	return nil
}

// validateCallbackDetails 只核对回调与已登记的业务字段一致性。
// v1 签名没有覆盖 amount/attach，不能由此证明回调字段未被篡改。
func validateCallbackDetails(order model.Order, callback model.PaymentCallback) error {
	if callback.Amount != order.Amount || callback.Attach != order.Attach {
		return model.ErrOrderConflict
	}
	return nil
}
