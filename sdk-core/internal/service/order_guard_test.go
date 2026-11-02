package service

import (
    "context"
    "errors"
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

func TestUnverifiedPlatformV1AlwaysRejected(t *testing.T) {
    for _,env:=range []string{"prod","test","review"} {
        order:=model.Order{AppID:"app1001",Env:env,GameOrderID:"game-1",Amount:600}
        callback:=model.PaymentCallback{OrderNo:"platform-1",GameOrderID:"game-1",Amount:600}
        err:=verifyTrustedPayment(context.Background(),order,callback)
        if !errors.Is(err,ErrUnsafePlatformProtocol) {
            t.Fatalf("environment %s unexpectedly accepted an unauthenticated payment: %v",env,err)
        }
    }
}
