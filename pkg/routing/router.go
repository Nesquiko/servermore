package routing

import (
	"context"
	"errors"

	"github.com/Nesquiko/servermore/pkg/caching"
	"github.com/Nesquiko/servermore/pkg/runner/grpc"
)

var ErrNoRunnerAvailable = errors.New("there is no healthy runner available")

type Router interface {
	Route(
		ctx context.Context,
		functionId string,
		cache caching.RoutingCache,
		db FunctionDb,
		runnerClientSupplier func(addr string) (grpc.RunnerClient, error),
	) (Routing, error)
}

type Routing struct {
	runnerAddr string
	instanceId string
}

type FunctionDb interface {
	FunctionPathById(ctx context.Context, id string) (string, error)
}
