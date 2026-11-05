package api

import (
    "context"
    "errors"
    "net/http"
    "testing"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

func TestMapServiceError(t *testing.T) {
    cases := []struct {
        feature string
        err error
        status int
        message string
    }{
        {"角色查询", model.ErrNotFound, 404, "渠道包不存在或未绑定该环境"},
        {"区服查询", model.ErrDisabled, 403, "当前渠道环境未开启区服查询"},
        {"角色查询", platform.ErrUnauthorized, 401, "平台请求签名验证失败"},
        {"封解禁", model.ErrInvalidConfig, 500, "封解禁服务配置不合法"},
        {"区服查询", model.ErrAdapterMissing, 500, "区服查询服务配置不合法"},
        {"角色查询", model.ErrUpstream, 502, "游戏角色查询接口调用失败"},
        {"角色查询", errors.Join(model.ErrUpstream, context.DeadlineExceeded), 504, "游戏角色查询接口请求超时"},
        {"封解禁", errors.Join(model.ErrUpstream, context.DeadlineExceeded), 504, "游戏封解禁接口请求超时，操作状态待确认"},
        {"封解禁", errors.New("offline"), http.StatusServiceUnavailable, "封解禁服务暂不可用"},
    }
    for _, tc := range cases {
        status, message := mapServiceError(tc.err, tc.feature)
        if status != tc.status || message != tc.message {
            t.Errorf("%s: status=%d, message=%s", tc.feature, status, message)
        }
    }
}
