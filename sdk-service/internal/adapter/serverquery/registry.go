package serverquery

import (
    "context"

    "hitall.com/middle-platform/sdk-service/internal/adapter"
    "hitall.com/middle-platform/sdk-service/internal/model"
)

type Adapter interface {
    Query(context.Context, model.Target, model.ServerQueryRequest) (model.ServerQueryResult, error)
}

type Registry = adapter.Registry[Adapter]

func NewRegistry(implementations map[string]Adapter) (*Registry, error) {
    return adapter.NewRegistry[Adapter](implementations)
}
