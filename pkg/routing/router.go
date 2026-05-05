package routing

import (
	"context"
	"errors"
	"fmt"

	"github.com/Nesquiko/servermore/pkg/caching"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"google.golang.org/grpc"
)

var ErrNoRunnerAvailable = errors.New("there is no healthy runner available")

type ErrPrepareInstance struct {
	FunctionId string
	RunnerAddr string
}

func (err *ErrPrepareInstance) Error() string {
	return fmt.Sprintf(
		"prepare instance for functionId %q on runner %q",
		err.FunctionId,
		err.RunnerAddr,
	)
}

type Router interface {
	Route(ctx context.Context, functionId string, cache caching.RoutingCache) (Routing, error)
}

type Routing struct {
	RunnerAddr string
	InstanceId string
}

type RunnerClientSupplier = func(addr string) (runnergrpc.RunnerClient, *grpc.ClientConn, error)

type FunctionDb interface {
	FunctionPathById(ctx context.Context, id string) (string, error)
}
