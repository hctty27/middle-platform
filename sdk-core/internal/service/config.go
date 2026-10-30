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

type Reader interface {
    Lookup(context.Context,string,string) (model.ChannelApp,*model.BoundGroup,error)
}

type ConfigService struct {
    reader Reader
    resolver *mapping.Resolver
}

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

    appConfig,err:=mapping.DecodeObject(app.Config)
    if err!=nil { return nil,fmt.Errorf("%w: channel app config: %v",model.ErrInvalidConfig,err) }
    regionConfig,err:=mapping.DecodeObject(group.RegionConfig)
    if err!=nil { return nil,fmt.Errorf("%w: region config: %v",model.ErrInvalidConfig,err) }
    groupConfig,err:=mapping.DecodeObject(group.Config)
    if err!=nil { return nil,fmt.Errorf("%w: server group config: %v",model.ErrInvalidConfig,err) }

    sources:=map[string]any{
        "env":env,
        "channel_app":map[string]any{"id":app.ID,"code":app.Code,"name":app.Name,"config":appConfig},
        "game":map[string]any{"code":app.GameCode},
        "region":map[string]any{"code":group.RegionCode,"config":regionConfig},
        "server_group":map[string]any{"code":group.ServerGroupCode,"config":groupConfig},
    }
    result,err:=s.resolver.Resolve(mapping.RouteCoreConfig,sources)
    if err!=nil { return nil,fmt.Errorf("%w: %v",model.ErrInvalidConfig,err) }
    return result,nil
}
