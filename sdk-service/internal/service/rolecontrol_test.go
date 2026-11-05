package service

import (
    "context"
    "net/http"
    "net/http/httptest"
    "strconv"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/rolecontrol"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/shared/signature"
)

type controlRoutes struct {
    target model.Target
    gotService string
}

func (r *controlRoutes) LookupService(_ context.Context, appID, env, code string) (model.Target, error) {
    r.gotService = code
    if appID != "110006" || env != "test" {
        return model.Target{}, model.ErrNotFound
    }
    return r.target, nil
}

type controlAdapter struct {
    calls int
    got model.RoleControlRequest
}

func (a *controlAdapter) Execute(_ context.Context, _ model.Target, in model.RoleControlRequest) error {
    a.calls++
    a.got = in
    return nil
}

func TestRoleControlServiceSignatureAndSelection(t *testing.T) {
    zero := int64(0)
    input := model.RoleControlRequest{Type: "close", OpenID: "u1", Duration: &zero}
    routes := &controlRoutes{target: model.Target{AdapterName: "standard", AppSecret: "platform-secret"}}
    impl := &controlAdapter{}
    registry, err := rolecontrol.NewRegistry(map[string]rolecontrol.Adapter{"standard": impl})
    if err != nil { t.Fatal(err) }
    service := &RoleControlService{Routes: routes, Adapters: registry}
    request := httptest.NewRequest(http.MethodPost, "/api/service/role/control/test", nil)
    ts := strconv.FormatInt(time.Now().Unix(), 10)
    nonce := "0011223344556677"
    request.Header.Set("x-app-id", "110006")
    request.Header.Set("x-timestamp", ts)
    request.Header.Set("x-nonce", nonce)
    vals := append([]string{"platform-secret", "110006", ts, nonce}, input.SignatureValues()...)
    request.Header.Set("x-signature", signature.SignValues(vals...))

    if err := service.Execute(request, "test", "110006", input); err != nil {
        t.Fatalf("合法封禁操作应被转发: %v", err)
    }
    if routes.gotService != "role_control" || impl.calls != 1 ||
        impl.got.Duration == nil || *impl.got.Duration != 0 {
        t.Fatalf("服务选择或永久封号丢失: %+v, %+v", routes, impl)
    }
    changed := input
    one := int64(1)
    changed.Duration = &one
    if err := service.Execute(request, "test", "110006", changed); err == nil {
        t.Fatal("篡改 duration 后签名应失败")
    }
    if impl.calls != 1 {
        t.Fatal("签名失败不得调用游戏")
    }
}
