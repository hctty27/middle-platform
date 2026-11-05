// Package serverquery 提供区服查询的可扩展适配器注册表。
package serverquery

import (
    "context"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type Adapter interface {
    Query(context.Context, model.Target, model.ServerQueryRequest) (model.ServerQueryResult, error)
}

type Registry struct {
    adapters map[string]Adapter
}

func NewRegistry(adapters map[string]Adapter) (*Registry, error) {
    if len(adapters) == 0 {
        return nil, fmt.Errorf("%w: 未注册区服查询实现", model.ErrAdapterMissing)
    }
    result := make(map[string]Adapter, len(adapters))
    for name, implementation := range adapters {
        if name == "" || implementation == nil {
            return nil, fmt.Errorf("%w: 区服查询适配器名称或实现无效", model.ErrAdapterMissing)
        }
        result[name] = implementation
    }
    return &Registry{adapters: result}, nil
}

func (r *Registry) Get(name string) (Adapter, error) {
    if r == nil {
        return nil, model.ErrAdapterMissing
    }
    implementation, exists := r.adapters[name]
    if !exists {
        return nil, fmt.Errorf("%w: 区服查询适配器 %s", model.ErrAdapterMissing, name)
    }
    return implementation, nil
}
