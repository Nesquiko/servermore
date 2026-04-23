package caching

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryCache_FunctionIdInstances_Empty(t *testing.T) {
	cache := NewInMemoryCache()
	instances, err := cache.FunctionIdInstances(context.Background(), "fn-1")
	require.NoError(t, err)
	assert.Empty(t, instances)
}

func TestInMemoryCache_FunctionIdInstances(t *testing.T) {
	cache := NewInMemoryCache()
	cache.SetInstance("fn-1", "inst-a", "runner:9000", 3)
	cache.SetInstance("fn-1", "inst-b", "runner:9000", 7)

	instances, err := cache.FunctionIdInstances(context.Background(), "fn-1")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"inst-a": 3, "inst-b": 7}, instances)
}

func TestInMemoryCache_FunctionIdInstances_UpdateQueueLen(t *testing.T) {
	cache := NewInMemoryCache()
	cache.SetInstance("fn-1", "inst-a", "runner:9000", 3)
	cache.UpdateInstanceQueueLen("fn-1", "inst-a", 10)

	instances, err := cache.FunctionIdInstances(context.Background(), "fn-1")
	require.NoError(t, err)
	assert.Equal(t, 10, instances["inst-a"])
}

func TestInMemoryCache_RunnerAddressOfInstance(t *testing.T) {
	cache := NewInMemoryCache()
	cache.SetInstance("fn-1", "inst-a", "runner:9000", 0)

	addr, err := cache.RunnerAddressOfInstance(context.Background(), "inst-a")
	require.NoError(t, err)
	assert.Equal(t, "runner:9000", addr)
}

func TestInMemoryCache_RunnerAddressOfInstance_NotFound(t *testing.T) {
	cache := NewInMemoryCache()

	_, err := cache.RunnerAddressOfInstance(context.Background(), "no-such-instance")
	assert.Error(t, err)
}

func TestInMemoryCache_RequestsPerRunner(t *testing.T) {
	cache := NewInMemoryCache()
	cache.SetRunnerRequests("runner:9000", 5)
	cache.SetRunnerRequests("runner:9001", 12)

	reqs, err := cache.RequestsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"runner:9000": 5, "runner:9001": 12}, reqs)
}

func TestInMemoryCache_RequestsPerRunner_Empty(t *testing.T) {
	cache := NewInMemoryCache()

	reqs, err := cache.RequestsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Empty(t, reqs)
}

func TestInMemoryCache_StatsPerRunner(t *testing.T) {
	cache := NewInMemoryCache()
	cache.SetRunnerStats("runner:9000", ResourceMetrics{CpuUtilization: 0.45, RamUsage: 0.60})

	stats, err := cache.StatsPerRunner(context.Background())
	require.NoError(t, err)
	require.Contains(t, stats, "runner:9000")
	assert.InDelta(t, 0.45, stats["runner:9000"].CpuUtilization, 0.001)
	assert.InDelta(t, 0.60, stats["runner:9000"].RamUsage, 0.001)
}

func TestInMemoryCache_StatsPerRunner_Empty(t *testing.T) {
	cache := NewInMemoryCache()

	stats, err := cache.StatsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stats)
}
