// Package service 实现 SDK 公开配置接口的业务校验。
package service

import (
    "context"
    "strings"
    "unicode/utf8"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// Reader 只提供公开配置接口需要的三个字段，不参与订单登记的配置查询。
type Reader interface {
    LookupPublicConfig(context.Context, string, string) (model.ConfigResponse, error)
}

// ConfigService 校验查询参数并读取固定的公开配置响应。
type ConfigService struct {
    reader Reader
}

// NewConfigService 注入公开配置读取接口。
func NewConfigService(reader Reader) *ConfigService {
    return &ConfigService{reader: reader}
}

// Get 按指定环境和渠道 AppID 查询配置，不使用响应字段映射。
func (s *ConfigService) Get(ctx context.Context, env, appID string) (model.ConfigResponse, error) {
    if env != "test" && env != "review" && env != "prod" {
        return model.ConfigResponse{}, model.ErrInvalidRequest
    }
    if strings.TrimSpace(appID) == "" || utf8.RuneCountInString(appID) > 128 {
        return model.ConfigResponse{}, model.ErrInvalidRequest
    }
    return s.reader.LookupPublicConfig(ctx, appID, env)
}
