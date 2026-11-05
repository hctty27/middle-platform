package payment

import (
	"errors"
	"net/http"
	"time"

	"github.com/hctty27/middle-platform/sdk-core/internal/model"
	"github.com/hctty27/middle-platform/shared/signature"
)

// ErrSignature 表示支付平台 V1 签名或回调时间戳不合法。
var ErrSignature = errors.New("平台支付回调签名验证失败")

// VerifyV1 严格匹配既有平台 V1 五字段签名，不过滤空字段。
// amount、attach、paid_at 不参与签名；调用方仍需独立校验金额。
func VerifyV1(h http.Header, body model.PaymentCallback, secret string) error {
	ts, nonce, provided := h.Get("x-timestamp"), h.Get("x-nonce"), h.Get("x-signature")
	if secret == "" || !signature.ValidHeaders(ts, nonce, provided, time.Now()) {
		return ErrSignature
	}
	expected := signature.SignAllValues(body.OrderNo, body.GameOrderID, secret, nonce, ts)
	if !signature.EqualHex(expected, provided) {
		return ErrSignature
	}
	return nil
}
