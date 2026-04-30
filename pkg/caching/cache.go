package caching

import "context"

type RoutingCache interface {
	// FunctionIdInstances returns a map of instance ids to their (last known) request queue length
	FunctionIdInstances(ctx context.Context, funcId string) (map[string]int, error)

	// RunnerAddressOfInstance finds the runner address on which the instance is running
	RunnerAddressOfInstance(ctx context.Context, instanceId string) (string, error)

	// RequestsPerRunner returns a map of runner address to the number of (last known) request count in all of it's queues.
	RequestsPerRunner(ctx context.Context) (map[string]int, error)

	// StatsPerRunner returns (last known) runner resource metrics
	StatsPerRunner(ctx context.Context) (map[string]ResourceMetrics, error)

	// SetInstance registers or updates a function instance and its runner.
	SetInstance(ctx context.Context, funcId, instanceId, runnerAddr string, queueLen int) error

	// UpsertRunnerHeartbeat replaces the cached heartbeat data for a runner.
	UpsertRunnerHeartbeat(
		ctx context.Context,
		runnerAddr string,
		queueDepths map[string]uint32,
		metrics ResourceMetrics,
	) error

	// RemoveRunner evicts a runner and all of its cached instances.
	RemoveRunner(ctx context.Context, runnerAddr string) error
}

type ResourceMetrics struct {
	CpuPercent        float64
	UnusedMemoryBytes uint64
}
