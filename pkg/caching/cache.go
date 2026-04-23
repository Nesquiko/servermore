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
}

type ResourceMetrics struct {
	CpuUtilization float32
	RamUsage       float32
}
