package rolecontrol

import (
    "context"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type Adapter interface {
    Execute(context.Context, model.Target, model.RoleControlRequest) error
}

type Registry = adapter.Registry[Adapter]

func NewRegistry(implementations map[string]Adapter) (*Registry, error) {
    return adapter.NewRegistry[Adapter](implementations)
}
