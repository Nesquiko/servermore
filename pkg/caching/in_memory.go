package caching

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// InMemoryCache is an in-memory implementation of [RoutingCache], suitable for testing.
type InMemoryCache struct {
	mu sync.RWMutex

	// funcInstances maps function ID -> instance ID -> request queue length
	funcInstances map[string]map[string]int

	// instanceRunner maps instance ID -> runner address
	instanceRunner map[string]string

	// runnerRequests maps runner address -> total request count across all its instances
	runnerRequests map[string]int

	// runnerStats maps runner address -> resource metrics
	runnerStats map[string]ResourceMetrics
}

func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		funcInstances:  make(map[string]map[string]int),
		instanceRunner: make(map[string]string),
		runnerRequests: make(map[string]int),
		runnerStats:    make(map[string]ResourceMetrics),
	}
}

var _ RoutingCache = (*InMemoryCache)(nil)

// FunctionIdInstances implements [RoutingCache].
func (c *InMemoryCache) FunctionIdInstances(
	_ context.Context,
	funcId string,
) (map[string]int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	instances, ok := c.funcInstances[funcId]
	if !ok {
		return map[string]int{}, nil
	}

	result := make(map[string]int, len(instances))
	maps.Copy(result, instances)
	return result, nil
}

// RunnerAddressOfInstance implements [RoutingCache].
func (c *InMemoryCache) RunnerAddressOfInstance(
	_ context.Context,
	instanceId string,
) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	addr, ok := c.instanceRunner[instanceId]
	if !ok {
		return "", fmt.Errorf("no runner found for instance %q", instanceId)
	}
	return addr, nil
}

// RequestsPerRunner implements [RoutingCache].
func (c *InMemoryCache) RequestsPerRunner(_ context.Context) (map[string]int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]int, len(c.runnerRequests))
	maps.Copy(result, c.runnerRequests)
	return result, nil
}

// StatsPerRunner implements [RoutingCache].
func (c *InMemoryCache) StatsPerRunner(_ context.Context) (map[string]ResourceMetrics, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]ResourceMetrics, len(c.runnerStats))
	maps.Copy(result, c.runnerStats)
	return result, nil
}

// SetInstance registers an instance for a function on a runner with an initial queue length.
func (c *InMemoryCache) SetInstance(funcId, instanceId, runnerAddr string, queueLen int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.funcInstances[funcId] == nil {
		c.funcInstances[funcId] = make(map[string]int)
	}
	c.funcInstances[funcId][instanceId] = queueLen
	c.instanceRunner[instanceId] = runnerAddr
}

// UpdateInstanceQueueLen updates the request queue length for a specific instance.
func (c *InMemoryCache) UpdateInstanceQueueLen(funcId, instanceId string, queueLen int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.funcInstances[funcId] != nil {
		c.funcInstances[funcId][instanceId] = queueLen
	}
}

// SetRunnerRequests updates the total request count for a runner.
func (c *InMemoryCache) SetRunnerRequests(runnerAddr string, count int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.runnerRequests[runnerAddr] = count
}

// SetRunnerStats updates the resource metrics for a runner.
func (c *InMemoryCache) SetRunnerStats(runnerAddr string, metrics ResourceMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.runnerStats[runnerAddr] = metrics
}
