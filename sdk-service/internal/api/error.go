package api

import (
    "context"
    "errors"
    "net/http"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

// mapServiceError 将 SDK Service 通用异常映射为保持兼容的 HTTP 状态与业务文案。
func mapServiceError(err error, feature string) (int, string) {
    switch {
    case errors.Is(err, model.ErrNotFound):
        return http.StatusNotFound, "渠道包不存在或未绑定该环境"
    case errors.Is(err, model.ErrDisabled):
        return http.StatusForbidden, "当前渠道环境未开启" + feature
    case errors.Is(err, platform.ErrUnauthorized):
        return http.StatusUnauthorized, "平台请求签名验证失败"
    case errors.Is(err, model.ErrUpstream):
        if errors.Is(err, context.DeadlineExceeded) {
            if feature == "封解禁" {
                return http.StatusGatewayTimeout, "游戏封解禁接口请求超时，操作状态待确认"
            }
            return http.StatusGatewayTimeout, "游戏" + feature + "接口请求超时"
        }
        return http.StatusBadGateway, "游戏" + feature + "接口调用失败"
    case errors.Is(err, model.ErrInvalidConfig), errors.Is(err, model.ErrAdapterMissing):
        return http.StatusInternalServerError, feature + "服务配置不合法"
    default:
        return http.StatusServiceUnavailable, feature + "服务暂不可用"
    }
}
