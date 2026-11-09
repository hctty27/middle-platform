package rolecontrol

import (
	"context"

	"hitall.com/middle-platform/sdk-service/internal/adapter"
	"hitall.com/middle-platform/sdk-service/internal/model"
)

type Adapter interface {
	Execute(context.Context, model.Target, model.RoleControlRequest) error
}

type Registry = adapter.Registry[Adapter]

func NewRegistry(implementations map[string]Adapter) (*Registry, error) {
	return adapter.NewRegistry[Adapter](implementations)
}
