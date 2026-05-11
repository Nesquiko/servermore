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
	// instanceFunction maps instance ID -> function ID
	instanceFunction map[string]string
	// instanceRunner maps instance ID -> runner address
	instanceRunner map[string]string
	// instanceQueue maps instance ID -> request queue length
	instanceQueue map[string]int
	// runnerInstances maps runner address -> active instance IDs
	runnerInstances map[string]map[string]struct{}

	// runnerRequests maps runner address -> total request count across all its instances
	runnerRequests map[string]int

	// runnerStats maps runner address -> resource metrics
	runnerStats map[string]ResourceMetrics
}

func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		funcInstances:    make(map[string]map[string]int),
		instanceFunction: make(map[string]string),
		instanceRunner:   make(map[string]string),
		instanceQueue:    make(map[string]int),
		runnerInstances:  make(map[string]map[string]struct{}),
		runnerRequests:   make(map[string]int),
		runnerStats:      make(map[string]ResourceMetrics),
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

// SetInstance registers an instance for a function on a runner with an initial queue length.
func (c *InMemoryCache) SetInstance(
	_ context.Context,
	funcId, instanceId, runnerAddr string,
	queueLen int,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	previousFunc := c.instanceFunction[instanceId]
	previousRunner := c.instanceRunner[instanceId]

	if previousFunc != "" && previousFunc != funcId {
		c.removeFunctionInstanceLocked(previousFunc, instanceId)
	}

	if previousRunner != "" && previousRunner != runnerAddr {
		c.removeRunnerInstanceLocked(previousRunner, instanceId)
	}

	if c.funcInstances[funcId] == nil {
		c.funcInstances[funcId] = make(map[string]int)
	}
	if c.runnerInstances[runnerAddr] == nil {
		c.runnerInstances[runnerAddr] = make(map[string]struct{})
	}

	c.funcInstances[funcId][instanceId] = queueLen
	c.instanceFunction[instanceId] = funcId
	c.instanceRunner[instanceId] = runnerAddr
	c.instanceQueue[instanceId] = queueLen
	c.runnerInstances[runnerAddr][instanceId] = struct{}{}

	c.recomputeRunnerRequestsLocked(runnerAddr)
	if previousRunner != "" && previousRunner != runnerAddr {
		c.recomputeRunnerRequestsLocked(previousRunner)
	}

	return nil
}

// UpsertRunnerHeartbeat replaces the cached heartbeat data for a runner.
func (c *InMemoryCache) UpsertRunnerHeartbeat(
	_ context.Context,
	runnerAddr string,
	queueDepths map[string]uint32,
	metrics ResourceMetrics,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	previousInstances := c.runnerInstances[runnerAddr]
	if previousInstances == nil {
		previousInstances = map[string]struct{}{}
	}

	newInstances := make(map[string]struct{}, len(queueDepths))
	for instanceID, queueDepth := range queueDepths {
		newInstances[instanceID] = struct{}{}

		previousRunner := c.instanceRunner[instanceID]
		if previousRunner != "" && previousRunner != runnerAddr {
			c.removeRunnerInstanceLocked(previousRunner, instanceID)
			c.recomputeRunnerRequestsLocked(previousRunner)
		}

		c.instanceRunner[instanceID] = runnerAddr
		c.instanceQueue[instanceID] = int(queueDepth)
		c.addRunnerInstanceLocked(runnerAddr, instanceID)

		funcID, ok := c.instanceFunction[instanceID]
		if !ok {
			continue
		}
		if c.funcInstances[funcID] == nil {
			c.funcInstances[funcID] = make(map[string]int)
		}
		c.funcInstances[funcID][instanceID] = int(queueDepth)
	}

	for instanceID := range previousInstances {
		if _, ok := newInstances[instanceID]; ok {
			continue
		}
		c.removeInstanceLocked(instanceID)
	}

	c.runnerInstances[runnerAddr] = newInstances
	c.recomputeRunnerRequestsLocked(runnerAddr)
	c.runnerStats[runnerAddr] = metrics

	return nil
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

// RemoveRunner evicts a runner and all of its cached instances.
func (c *InMemoryCache) RemoveRunner(_ context.Context, runnerAddr string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	instances := c.runnerInstances[runnerAddr]
	for instanceID := range instances {
		c.removeInstanceLocked(instanceID)
	}

	delete(c.runnerInstances, runnerAddr)
	delete(c.runnerRequests, runnerAddr)
	delete(c.runnerStats, runnerAddr)

	return nil
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

// CachedInstancesCount implements [RoutingCache].
func (c *InMemoryCache) CachedInstancesCount(_ context.Context) (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.instanceRunner), nil
}

// UpdateInstanceQueueLen updates the request queue length for a specific instance.
func (c *InMemoryCache) UpdateInstanceQueueLen(funcId, instanceId string, queueLen int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.funcInstances[funcId] == nil {
		c.funcInstances[funcId] = make(map[string]int)
	}
	c.funcInstances[funcId][instanceId] = queueLen
	c.instanceFunction[instanceId] = funcId
	c.instanceQueue[instanceId] = queueLen

	if runnerAddr := c.instanceRunner[instanceId]; runnerAddr != "" {
		c.recomputeRunnerRequestsLocked(runnerAddr)
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

func (c *InMemoryCache) addRunnerInstanceLocked(runnerAddr, instanceID string) {
	if c.runnerInstances[runnerAddr] == nil {
		c.runnerInstances[runnerAddr] = make(map[string]struct{})
	}
	c.runnerInstances[runnerAddr][instanceID] = struct{}{}
}

func (c *InMemoryCache) removeInstanceLocked(instanceID string) {
	funcID, hasFunc := c.instanceFunction[instanceID]
	runnerAddr, hasRunner := c.instanceRunner[instanceID]

	if hasFunc {
		c.removeFunctionInstanceLocked(funcID, instanceID)
	}
	if hasRunner {
		c.removeRunnerInstanceLocked(runnerAddr, instanceID)
		c.recomputeRunnerRequestsLocked(runnerAddr)
	}

	delete(c.instanceFunction, instanceID)
	delete(c.instanceRunner, instanceID)
	delete(c.instanceQueue, instanceID)
}

func (c *InMemoryCache) removeFunctionInstanceLocked(funcID, instanceID string) {
	instances := c.funcInstances[funcID]
	if instances == nil {
		return
	}

	delete(instances, instanceID)
	if len(instances) == 0 {
		delete(c.funcInstances, funcID)
	}
}

func (c *InMemoryCache) removeRunnerInstanceLocked(runnerAddr, instanceID string) {
	instances := c.runnerInstances[runnerAddr]
	if instances == nil {
		return
	}

	delete(instances, instanceID)
}

func (c *InMemoryCache) recomputeRunnerRequestsLocked(runnerAddr string) {
	instances, ok := c.runnerInstances[runnerAddr]
	if !ok {
		delete(c.runnerRequests, runnerAddr)
		return
	}

	total := 0
	for instanceID := range instances {
		total += c.instanceQueue[instanceID]
	}
	c.runnerRequests[runnerAddr] = total
}
