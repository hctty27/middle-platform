package rolequery

import (
    "context"

    "hitall.com/middle-platform/sdk-service/internal/adapter"
    "hitall.com/middle-platform/sdk-service/internal/model"
)

type Adapter interface {
    Query(context.Context, model.Target, model.RoleQueryRequest) ([]model.Role, error)
}

type Registry = adapter.Registry[Adapter]

func NewRegistry(implementations map[string]Adapter) (*Registry, error) {
    return adapter.NewRegistry[Adapter](implementations)
}
