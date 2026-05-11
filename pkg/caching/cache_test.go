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
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-a", "runner:9000", 3))
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-b", "runner:9000", 7))

	instances, err := cache.FunctionIdInstances(context.Background(), "fn-1")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"inst-a": 3, "inst-b": 7}, instances)
}

func TestInMemoryCache_FunctionIdInstances_UpdateQueueLen(t *testing.T) {
	cache := NewInMemoryCache()
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-a", "runner:9000", 3))
	cache.UpdateInstanceQueueLen("fn-1", "inst-a", 10)

	instances, err := cache.FunctionIdInstances(context.Background(), "fn-1")
	require.NoError(t, err)
	assert.Equal(t, 10, instances["inst-a"])
}

func TestInMemoryCache_RunnerAddressOfInstance(t *testing.T) {
	cache := NewInMemoryCache()
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-a", "runner:9000", 0))

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
	cache.SetRunnerStats("runner:9000", ResourceMetrics{CpuPercent: 45.5, UnusedMemoryBytes: 1024})

	stats, err := cache.StatsPerRunner(context.Background())
	require.NoError(t, err)
	require.Contains(t, stats, "runner:9000")
	assert.InDelta(t, 45.5, stats["runner:9000"].CpuPercent, 0.001)
	assert.Equal(t, uint64(1024), stats["runner:9000"].UnusedMemoryBytes)
}

func TestInMemoryCache_StatsPerRunner_Empty(t *testing.T) {
	cache := NewInMemoryCache()

	stats, err := cache.StatsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stats)
}

func TestInMemoryCache_CachedInstancesCount(t *testing.T) {
	cache := NewInMemoryCache()
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-a", "runner:9000", 3))
	require.NoError(t, cache.SetInstance(context.Background(), "fn-2", "inst-b", "runner:9001", 7))

	count, err := cache.CachedInstancesCount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestInMemoryCache_UpsertRunnerHeartbeat(t *testing.T) {
	cache := NewInMemoryCache()
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-a", "runner:9000", 3))
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-b", "runner:9000", 7))
	require.NoError(t, cache.SetInstance(context.Background(), "fn-2", "inst-c", "runner:9001", 4))

	require.NoError(
		t,
		cache.UpsertRunnerHeartbeat(
			context.Background(),
			"runner:9000",
			map[string]uint32{"inst-a": 10},
			ResourceMetrics{CpuPercent: 45.5, UnusedMemoryBytes: 1024},
		),
	)
	require.NoError(
		t,
		cache.UpsertRunnerHeartbeat(
			context.Background(),
			"runner:9001",
			map[string]uint32{},
			ResourceMetrics{CpuPercent: 12.25, UnusedMemoryBytes: 2048},
		),
	)

	instances, err := cache.FunctionIdInstances(context.Background(), "fn-1")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"inst-a": 10}, instances)

	instances, err = cache.FunctionIdInstances(context.Background(), "fn-2")
	require.NoError(t, err)
	assert.Empty(t, instances)

	reqs, err := cache.RequestsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"runner:9000": 10, "runner:9001": 0}, reqs)

	stats, err := cache.StatsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ResourceMetrics{CpuPercent: 45.5, UnusedMemoryBytes: 1024}, stats["runner:9000"])
	assert.Equal(t, ResourceMetrics{CpuPercent: 12.25, UnusedMemoryBytes: 2048}, stats["runner:9001"])

	addr, err := cache.RunnerAddressOfInstance(context.Background(), "inst-a")
	require.NoError(t, err)
	assert.Equal(t, "runner:9000", addr)

	_, err = cache.RunnerAddressOfInstance(context.Background(), "inst-b")
	assert.Error(t, err)
}

func TestInMemoryCache_RemoveRunner(t *testing.T) {
	cache := NewInMemoryCache()
	require.NoError(t, cache.SetInstance(context.Background(), "fn-1", "inst-a", "runner:9000", 3))
	cache.SetRunnerStats("runner:9000", ResourceMetrics{CpuPercent: 45.5, UnusedMemoryBytes: 1024})

	require.NoError(t, cache.RemoveRunner(context.Background(), "runner:9000"))

	instances, err := cache.FunctionIdInstances(context.Background(), "fn-1")
	require.NoError(t, err)
	assert.Empty(t, instances)

	reqs, err := cache.RequestsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Empty(t, reqs)

	stats, err := cache.StatsPerRunner(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stats)

	_, err = cache.RunnerAddressOfInstance(context.Background(), "inst-a")
	assert.Error(t, err)
}
