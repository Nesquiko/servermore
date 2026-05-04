package routing

import (
	"context"
	"errors"

	"github.com/Nesquiko/servermore/pkg/caching"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"google.golang.org/grpc"
)

var ErrNoRunnerAvailable = errors.New("there is no healthy runner available")

type Router interface {
	Route(
		ctx context.Context,
		functionId string,
		cache caching.RoutingCache,
		db FunctionDb,
		runnerClientSupplier RunnerClientSupplier,
	) (Routing, error)
}

type Routing struct {
	RunnerAddr string
	InstanceId string
}

type RunnerClientSupplier = func(addr string) (runnergrpc.RunnerClient, *grpc.ClientConn, error)

type FunctionDb interface {
	FunctionPathById(ctx context.Context, id string) (string, error)
}
