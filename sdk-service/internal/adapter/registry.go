package adapter

import (
    "fmt"
    "reflect"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

// Registry 按名称解析某一类业务适配器；注册后不允许修改内部映射。
type Registry[T any] struct {
    adapters map[string]T
}

func NewRegistry[T any](implementations map[string]T) (*Registry[T], error) {
    if len(implementations) == 0 {
        return nil, fmt.Errorf("%w: 未注册任何实现", model.ErrAdapterMissing)
    }
    registered := make(map[string]T, len(implementations))
    for name, implementation := range implementations {
        if name == "" || isNil(implementation) {
            return nil, fmt.Errorf("%w: 适配器名称为空或实现无效", model.ErrAdapterMissing)
        }
        registered[name] = implementation
    }
    return &Registry[T]{adapters: registered}, nil
}

func (r *Registry[T]) Get(name string) (T, error) {
    var zero T
    if r == nil {
        return zero, model.ErrAdapterMissing
    }
    implementation, ok := r.adapters[name]
    if !ok {
        return zero, fmt.Errorf("%w: %s", model.ErrAdapterMissing, name)
    }
    return implementation, nil
}

func isNil[T any](implementation T) bool {
    value := reflect.ValueOf(implementation)
    if !value.IsValid() {
        return true
    }
    switch value.Kind() {
    case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
        return value.IsNil()
    }
    return false
}
