// Package service 实现 SDK 配置查询和接口级返回映射。
package service

import (
    "context"
    "fmt"
    "strings"
    "unicode/utf8"

    "github.com/hctty27/middle-platform/sdk-core/internal/mapping"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// Reader 抽象查询绑定区服组和各级配置所需的数据源。
type Reader interface {
    Lookup(context.Context,string,string) (model.ChannelApp,*model.BoundGroup,error)
}

// ConfigService 使用只读数据源和预编译映射组装 SDK 返回数据。
type ConfigService struct {
    reader Reader
    resolver *mapping.Resolver
}

// NewConfigService 注入查询接口与字段映射解析器，不在构造时请求数据库。
func NewConfigService(reader Reader,resolver *mapping.Resolver) *ConfigService {
    return &ConfigService{reader:reader,resolver:resolver}
}

// Get 只按 env 选择数据，不为不同环境定义不同的映射模板。
func (s *ConfigService) Get(ctx context.Context,env,appID string) (map[string]any,error) {
    if env!="test" && env!="review" && env!="prod" {
        return nil,model.ErrInvalidRequest
    }
    if strings.TrimSpace(appID)=="" || utf8.RuneCountInString(appID)>128 {
        return nil,model.ErrInvalidRequest
    }

    app,group,err:=s.reader.Lookup(ctx,appID,env)
    if err!=nil { return nil,err }
    if group==nil { return nil,model.ErrNotFound }

    // 普通字段始终可用；只有映射引用 config 时才解析对应的 JSON。
    appSource := map[string]any{"id":app.ID,"code":app.Code,"name":app.Name}
    gameSource := map[string]any{"code":app.GameCode}
    regionSource := map[string]any{"code":group.RegionCode}
    groupSource := map[string]any{"code":group.ServerGroupCode}

    const route = mapping.RouteCoreConfig
    configInputs := []struct {
        name string
        raw []byte
        target map[string]any
    }{
        {"channel_app",app.Config,appSource},
        {"game",app.GameConfig,gameSource},
        {"region",group.RegionConfig,regionSource},
        {"server_group",group.Config,groupSource},
    }
    for _, input := range configInputs {
        if !s.resolver.UsesConfig(route,input.name) {
            continue
        }
        config,err:=mapping.DecodeObject(input.raw)
        if err!=nil {
            return nil,fmt.Errorf("%w: %s 配置错误: %v",model.ErrInvalidConfig,input.name,err)
        }
        // 渠道包 JSON 允许按需映射普通字段；平台支付密钥不得通过公开配置接口泄露。
        if input.name=="channel_app" { delete(config,"appSecret") }
        if input.name=="game" { delete(config,"gameSecret") }
        input.target["config"]=config
    }

    sources:=map[string]any{
        "env":env,
        "channel_app":appSource,
        "game":gameSource,
        "region":regionSource,
        "server_group":groupSource,
    }
    result,err:=s.resolver.Resolve(mapping.RouteCoreConfig,sources)
    if err!=nil { return nil,fmt.Errorf("%w: %v",model.ErrInvalidConfig,err) }
    return result,nil
}
