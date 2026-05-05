package routing

import (
	"context"
	"fmt"

	"github.com/Nesquiko/servermore/pkg/caching"
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

	healthyRunner := pickLeastLoadedRunner(runners, n.runnerOverloadThreshold)
	if healthyRunner == "" {
		return Routing{}, ErrNoRunnerAvailable
	}

	return Routing{}, &ErrPrepareInstance{FunctionId: functionId, RunnerAddr: healthyRunner}
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
			return &Routing{RunnerAddr: runnerAddr, InstanceId: instId}, nil
		}
	}

	return nil, nil
}

const MaxInt = int((^uint(0)) >> 1)

func pickLeastLoadedRunner(
	requestsPerRunner map[string]int,
	overloadThreshold int,
) string {
	leastLoaded := ""
	min := MaxInt

	for runnerAddr, requests := range requestsPerRunner {
		if requests >= overloadThreshold {
			continue
		}
		if requests < min {
			leastLoaded = runnerAddr
			min = requests
		}
	}

	return leastLoaded
}
