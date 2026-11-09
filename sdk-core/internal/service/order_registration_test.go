package service

import (
    "context"
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "hitall.com/middle-platform/sdk-core/internal/auth"
    "hitall.com/middle-platform/sdk-core/internal/model"
)

const registrationTestAppID = "app1001"

type countingRegistrationConfigReader struct {
    calls int
    app model.ChannelApp
    group *model.BoundGroup
}

func (r *countingRegistrationConfigReader) Lookup(_ context.Context, _, _ string) (model.ChannelApp, *model.BoundGroup, error) {
    r.calls++
    return r.app, r.group, nil
}

type registrationOrderRepository struct {
    existing *model.Order
    findCalls int
    registerCalls int
    registered model.Order
}

func (r *registrationOrderRepository) Find(_ context.Context, appID, gameOrderID string) (model.Order, error) {
    r.findCalls++
    if r.existing == nil || r.existing.AppID != appID || r.existing.GameOrderID != gameOrderID {
        return model.Order{}, model.ErrOrderNotFound
    }
    return *r.existing, nil
}

func (r *registrationOrderRepository) Register(_ context.Context, order model.Order) error {
    r.registerCalls++
    r.registered = order
    return nil
}

func (*registrationOrderRepository) ConfirmPayment(context.Context, int64, string, time.Time) error {
    return nil
}

func (*registrationOrderRepository) CompleteNotify(context.Context, int64, int, error) error {
    return nil
}

func (*registrationOrderRepository) ClaimDue(context.Context) (*model.Order, error) {
    return nil, nil
}

type registrationTestNotifier struct {
    validateCalls int
}

func (n *registrationTestNotifier) ValidateTarget(string) error {
    n.validateCalls++
    return nil
}

func (*registrationTestNotifier) Send(context.Context, model.Order, []byte) error {
    return nil
}

func prepareRegistrationTest(t *testing.T, existing *model.Order) (
    *OrderService,
    *countingRegistrationConfigReader,
    *registrationOrderRepository,
    *registrationTestNotifier,
    VerifiedRegistration,
    model.RegisterOrder,
) {
    t.Helper()

    configReader := &countingRegistrationConfigReader{
        app: model.ChannelApp{
            GameID: 42,
            Code: registrationTestAppID,
            GameSDKConfig: []byte(`{"gameSecret":"game-secret"}`),
        },
        group: &model.BoundGroup{
            RegionSDKConfig: []byte(`{"payNotifyUrl":"https://game.example/pay"}`),
        },
    }
    orders := &registrationOrderRepository{existing: existing}
    notifier := &registrationTestNotifier{}
    svc := &OrderService{
        ConfigReader: configReader,
        Orders: orders,
        Notifier: notifier,
    }

    body := []byte(`{"game_order_id":"order-001","amount":600,"attach":"purchase"}`)
    timestamp := strconv.FormatInt(time.Now().Unix(), 10)
    nonce := "0123456789abcdef"
    signature, err := auth.SignGame([]byte("game-secret"), registrationTestAppID, timestamp, nonce, body)
    if err != nil {
        t.Fatalf("生成测试签名失败: %v", err)
    }

    request := httptest.NewRequest(http.MethodPost, "/api/core/orders/prod/"+registrationTestAppID, nil)
    request.SetPathValue("env", "prod")
    request.SetPathValue("appid", registrationTestAppID)
    request.Header.Set("x-app-id", registrationTestAppID)
    request.Header.Set("x-timestamp", timestamp)
    request.Header.Set("x-nonce", nonce)
    request.Header.Set("x-signature", signature)

    verified, err := svc.VerifyRegistration(request, body, "prod", registrationTestAppID)
    if err != nil {
        t.Fatalf("订单验签失败: %v", err)
    }

    return svc, configReader, orders, notifier, verified, model.RegisterOrder{
        GameOrderID: "order-001",
        Amount: 600,
        Attach: "purchase",
    }
}

func TestRegisterNewOrderReusesVerifiedConfig(t *testing.T) {
    svc, configReader, orders, notifier, verified, req := prepareRegistrationTest(t, nil)

    if err := svc.Register(context.Background(), "prod", registrationTestAppID, req, verified); err != nil {
        t.Fatalf("登记新订单失败: %v", err)
    }

    if configReader.calls != 1 {
        t.Fatalf("Store.Lookup 调用次数错误: got=%d want=1", configReader.calls)
    }
    if orders.findCalls != 1 || orders.registerCalls != 1 {
        t.Fatalf("订单仓储调用次数错误: Find=%d Register=%d", orders.findCalls, orders.registerCalls)
    }
    if notifier.validateCalls != 1 {
        t.Fatalf("通知地址校验次数错误: got=%d want=1", notifier.validateCalls)
    }
    if orders.registered.GameID != 42 || orders.registered.GameNotifyURL != "https://game.example/pay" {
        t.Fatalf("订单没有使用验签时的配置快照: %+v", orders.registered)
    }
}

func TestRegisterDuplicateOrderReusesVerifiedConfig(t *testing.T) {
    existing := &model.Order{
        ID: 7,
        AppID: registrationTestAppID,
        GameID: 42,
        Env: "prod",
        GameOrderID: "order-001",
        Amount: 600,
        Attach: "purchase",
    }
    svc, configReader, orders, notifier, verified, req := prepareRegistrationTest(t, existing)

    if err := svc.Register(context.Background(), "prod", registrationTestAppID, req, verified); err != nil {
        t.Fatalf("重复订单幂等登记失败: %v", err)
    }

    if configReader.calls != 1 {
        t.Fatalf("Store.Lookup 调用次数错误: got=%d want=1", configReader.calls)
    }
    if orders.findCalls != 1 || orders.registerCalls != 0 {
        t.Fatalf("重复订单不应插入: Find=%d Register=%d", orders.findCalls, orders.registerCalls)
    }
    if notifier.validateCalls != 0 {
        t.Fatalf("重复订单不应重新读取或校验通知配置: got=%d", notifier.validateCalls)
    }
}
