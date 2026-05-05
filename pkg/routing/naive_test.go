package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/Nesquiko/servermore/pkg/caching"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNaiveRouter_RoutesToHealthyExistingInstance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := caching.NewInMemoryCache()
	require.NoError(t, cache.SetInstance(ctx, "fn-1", "inst-1", "127.0.0.1:5001", 0))

	router := NewNaiveRouter(1, 1)

	routingData, err := router.Route(ctx, "fn-1", cache)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:5001", routingData.RunnerAddr)
	assert.Equal(t, "inst-1", routingData.InstanceId)
}

func TestNaiveRouter_SkipsOverloadedInstancesAndSignalToCreateNewOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := caching.NewInMemoryCache()
	require.NoError(t, cache.SetInstance(ctx, "fn-1", "inst-overloaded", "127.0.0.1:5001", 2))
	require.NoError(t, cache.SetInstance(ctx, "fn-2", "inst-healthy", "127.0.0.1:5002", 0))

	router := NewNaiveRouter(1, 1)
	routingData, err := router.Route(ctx, "fn-1", cache)
	require.Error(t, err)
	assert.Equal(t, Routing{}, routingData)

	var prepareErr *ErrPrepareInstance
	require.True(t, errors.As(err, &prepareErr))
	assert.Equal(t, "fn-1", prepareErr.FunctionId)
	assert.Equal(t, "127.0.0.1:5002", prepareErr.RunnerAddr)
}

func TestNaiveRouter_PicksLeastLoadedRunner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := caching.NewInMemoryCache()
	require.NoError(t, cache.SetInstance(ctx, "fn-a", "inst-a", "127.0.0.1:5001", 5))
	require.NoError(t, cache.SetInstance(ctx, "fn-b", "inst-b", "127.0.0.1:5002", 1))
	require.NoError(t, cache.SetInstance(ctx, "fn-c", "inst-c", "127.0.0.1:5003", 3))

	router := NewNaiveRouter(1, 10)
	routingData, err := router.Route(ctx, "fn-target", cache)
	require.Error(t, err)
	assert.Equal(t, Routing{}, routingData)

	var prepareErr *ErrPrepareInstance
	require.True(t, errors.As(err, &prepareErr))
	assert.Equal(t, "fn-target", prepareErr.FunctionId)
	assert.Equal(t, "127.0.0.1:5002", prepareErr.RunnerAddr)
}

func TestNaiveRouter_ReturnsErrWhenNoRunnerIsHealthy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := caching.NewInMemoryCache()
	require.NoError(t, cache.SetInstance(ctx, "fn-a", "inst-a", "127.0.0.1:5001", 5))
	require.NoError(t, cache.SetInstance(ctx, "fn-b", "inst-b", "127.0.0.1:5002", 7))

	router := NewNaiveRouter(1, 1)
	_, err := router.Route(ctx, "fn-target", cache)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoRunnerAvailable)
}
