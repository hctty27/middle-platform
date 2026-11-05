// Package rolecontrol 定义封解禁操作的游戏适配器与注册表。
package rolecontrol

import (
    "context"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type Adapter interface {
    Execute(context.Context, model.Target, model.RoleControlRequest) error
}

type Registry struct {
    adapters map[string]Adapter
}

func NewRegistry(adapters map[string]Adapter) (*Registry, error) {
    if len(adapters) == 0 {
        return nil, fmt.Errorf("%w: 未注册封解禁适配器", model.ErrAdapterMissing)
    }
    result := make(map[string]Adapter, len(adapters))
    for name, implementation := range adapters {
        if name == "" || implementation == nil {
            return nil, fmt.Errorf("%w: 封解禁适配器名称或实现无效", model.ErrAdapterMissing)
        }
        result[name] = implementation
    }
    return &Registry{adapters: result}, nil
}

func (r *Registry) Get(name string) (Adapter, error) {
    if r == nil {
        return nil, model.ErrAdapterMissing
    }
    result, ok := r.adapters[name]
    if !ok {
        return nil, fmt.Errorf("%w: 封解禁适配器 %s", model.ErrAdapterMissing, name)
    }
    return result, nil
}
