package routing

import (
	"context"
	"fmt"

	"github.com/Nesquiko/servermore/pkg/caching"
	"github.com/Nesquiko/servermore/pkg/runner/grpc"
)

type NaiveRouter struct {
	instanceOverloadThreshold int
	runnerOverloadThreshold   int
}

func NewNaiveRouter(instanceOverloadThreshold, runnerOverloadThreshold int) *NaiveRouter {
	return &NaiveRouter{
		instanceOverloadThreshold: instanceOverloadThreshold,
		runnerOverloadThreshold:   runnerOverloadThreshold,
	}
}

var _ Router = (*NaiveRouter)(nil)

// Route implements [Router].
func (n *NaiveRouter) Route(
	ctx context.Context,
	functionId string,
	cache caching.RoutingCache,
	db FunctionDb,
	runnerClientSupplier func(addr string) (grpc.RunnerClient, error),
) (Routing, error) {
	instances, err := cache.FunctionIdInstances(ctx, functionId)
	if err != nil {
		return Routing{}, fmt.Errorf("error getting instances for functionId: %w", err)
	}

	routing, err := pickHealthyInstance(ctx, instances, n.instanceOverloadThreshold, cache)
	if err != nil {
		return Routing{}, fmt.Errorf("failed to find healthy instance: %w", err)
	} else if routing != nil {
		return *routing, nil
	}

	// there is no runner with instance under threshold, must create new instance
	runners, err := cache.RequestsPerRunner(ctx)
	if err != nil {
		return Routing{}, fmt.Errorf("failed to read requests per runner: %w", err)
	}

	functionPath, err := db.FunctionPathById(ctx, functionId)
	if err != nil {
		return Routing{}, fmt.Errorf("failed to read function by id %q: %w", functionId, err)
	}

	healthyRunner := pickHealthyRunner(runners, n.runnerOverloadThreshold)
	if healthyRunner == "" {
		return Routing{}, ErrNoRunnerAvailable
	}

	runnerClient, err := runnerClientSupplier(healthyRunner)
	if err != nil {
		return Routing{}, fmt.Errorf("failed to construct runner client: %w", err)
	}

	resp, err := runnerClient.PrepareFunctionInstance(
		ctx,
		&grpc.PrepareInstanceRequest{FunctionId: functionId, FunctionPath: functionPath},
	)
	if err != nil {
		return Routing{}, fmt.Errorf("prepare instance call failed: %w", err)
	}

	return Routing{runnerAddr: healthyRunner, instanceId: resp.InstanceId}, nil
}

func pickHealthyInstance(
	ctx context.Context,
	instances map[string]int,
	overloadThreshold int,
	cache caching.RoutingCache,
) (*Routing, error) {
	for instId, queueLen := range instances {
		if queueLen < overloadThreshold {
			runnerAddr, err := cache.RunnerAddressOfInstance(ctx, instId)
			if err != nil {
				return nil, fmt.Errorf(
					"error getting runner address of instance %q: %w",
					instId, err,
				)
			}
			return &Routing{runnerAddr: runnerAddr, instanceId: instId}, nil
		}
	}

	return nil, nil
}

func pickHealthyRunner(
	requestsPerRunner map[string]int,
	overloadThreshold int,
) string {
	for runnerAddr, requests := range requestsPerRunner {
		if requests < overloadThreshold {
			return runnerAddr
		}
	}

	return ""
}
