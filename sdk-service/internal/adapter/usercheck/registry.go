// Package usercheck 定义按业务拆分的游戏适配器和注册表。
package usercheck

import (
    "context"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type Adapter interface {
    Check(context.Context, model.Target, model.UserCheckRequest) (model.UserCheckResult, error)
}

type Registry struct { adapters map[string]Adapter }

func NewRegistry(adapters map[string]Adapter) (*Registry, error) {
    if len(adapters) == 0 { return nil, fmt.Errorf("%w: 未注册任何实现", model.ErrAdapterMissing) }
    result := make(map[string]Adapter, len(adapters))
    for name, adapter := range adapters {
        if name == "" || adapter == nil { return nil, fmt.Errorf("%w: 名称为空或实现无效", model.ErrAdapterMissing) }
        result[name] = adapter
    }
    return &Registry{adapters: result}, nil
}

func (r *Registry) Get(name string) (Adapter, error) {
    if r == nil { return nil, model.ErrAdapterMissing }
    adapter, ok := r.adapters[name]
    if !ok { return nil, fmt.Errorf("%w: %s", model.ErrAdapterMissing, name) }
    return adapter, nil
}
