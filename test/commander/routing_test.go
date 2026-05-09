package commander_test

import (
	"strconv"
	"testing"

	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

func TestRouteFunction_PreparesThenReusesExistingInstance(t *testing.T) {
	ctx := t.Context()

	originalRunnerRequests, err := TestCache.RequestsPerRunner(ctx)
	require.NoError(t, err)
	for runnerAddr := range originalRunnerRequests {
		require.NoError(t, TestCache.RemoveRunner(ctx, runnerAddr))
	}
	t.Cleanup(func() {
		for runnerAddr, requests := range originalRunnerRequests {
			TestCache.SetRunnerRequests(runnerAddr, requests)
		}
	})

	t.Run("returns unavailable when no runner available", func(t *testing.T) {
		client := newCommanderClient(t)
		routingResp, err := client.RouteFunction(
			ctx,
			&commandergrpc.RouteFunctionRequest{FunctionId: "missing-function-for-unavailable"},
		)
		require.Error(t, err)
		assert.Nil(t, routingResp)

		grpcStatus, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, grpcStatus.Code())
	})

	stubRunner := testutils.RunStubRunner(ctx)
	stubRunner.SetPrepare(&runnergrpc.PrepareInstanceResponse{InstanceId: "prepared-inst-1"}, nil)
	t.Cleanup(stubRunner.Close)
	t.Cleanup(func() {
		require.NoError(t, TestCache.RemoveRunner(ctx, stubRunner.GrpcAddr()))
	})
	TestCache.SetRunnerRequests(stubRunner.GrpcAddr(), 0)

	created := submitFunction(
		t,
		"route-function-prepares-first",
		"route-function-prepares-first.bin",
		randomBinary(t, 512),
	)

	t.Run("prepares new instance", func(t *testing.T) {
		client := newCommanderClient(t)
		routingResp, err := client.RouteFunction(
			ctx,
			&commandergrpc.RouteFunctionRequest{FunctionId: strconv.FormatInt(created.Id, 10)},
		)
		require.NoError(t, err)
		assert.Equal(t, stubRunner.GrpcAddr(), routingResp.GetRunnerAddr())
		assert.Equal(t, "prepared-inst-1", routingResp.GetInstanceId())
		assert.EqualValues(t, 1, stubRunner.PrepareCallsCount())
	})

	t.Run("reuses existing instance", func(t *testing.T) {
		require.NoError(t, TestCache.SetInstance(
			ctx,
			strconv.FormatInt(created.Id, 10),
			"reused-inst-1",
			stubRunner.GrpcAddr(),
			0,
		))

		client := newCommanderClient(t)
		routingResp, err := client.RouteFunction(ctx, &commandergrpc.RouteFunctionRequest{
			FunctionId: strconv.FormatInt(created.Id, 10),
		})
		require.NoError(t, err)
		assert.Equal(t, stubRunner.GrpcAddr(), routingResp.GetRunnerAddr())
		assert.Equal(t, "reused-inst-1", routingResp.GetInstanceId())
		assert.EqualValues(t, 1, stubRunner.PrepareCallsCount())
	})

	t.Run("uses least loaded runner", func(t *testing.T) {
		leastLoadedRunner := testutils.RunStubRunner(ctx)
		leastLoadedRunner.SetPrepare(
			&runnergrpc.PrepareInstanceResponse{InstanceId: "least-loaded-inst-1"},
			nil,
		)
		t.Cleanup(leastLoadedRunner.Close)
		t.Cleanup(func() {
			require.NoError(t, TestCache.RemoveRunner(ctx, leastLoadedRunner.GrpcAddr()))
		})

		TestCache.SetRunnerRequests(stubRunner.GrpcAddr(), 0)
		TestCache.SetRunnerRequests(leastLoadedRunner.GrpcAddr(), 3)

		leastLoadedFunction := submitFunction(
			t,
			"route-function-uses-least-loaded",
			"route-function-uses-least-loaded.bin",
			randomBinary(t, 512),
		)

		client := newCommanderClient(t)
		routingResp, err := client.RouteFunction(ctx, &commandergrpc.RouteFunctionRequest{
			FunctionId: strconv.FormatInt(leastLoadedFunction.Id, 10),
		})
		require.NoError(t, err)
		assert.Equal(t, stubRunner.GrpcAddr(), routingResp.GetRunnerAddr())
		assert.Equal(t, "prepared-inst-1", routingResp.GetInstanceId())
		assert.EqualValues(t, 2, stubRunner.PrepareCallsCount())
		assert.EqualValues(t, 0, leastLoadedRunner.PrepareCallsCount())
	})
}

func TestRouteFunction_ReturnsNotFoundWhenFunctionMissing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	stubRunner := testutils.RunStubRunner(ctx)
	t.Cleanup(stubRunner.Close)
	t.Cleanup(func() {
		require.NoError(t, TestCache.RemoveRunner(ctx, stubRunner.GrpcAddr()))
	})

	TestCache.SetRunnerRequests(stubRunner.GrpcAddr(), 0)

	client := newCommanderClient(t)
	routingResp, err := client.RouteFunction(
		ctx,
		&commandergrpc.RouteFunctionRequest{FunctionId: strconv.FormatInt(999999999999, 10)},
	)
	require.Error(t, err)
	assert.Nil(t, routingResp)

	grpcStatus, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, grpcStatus.Code())
	assert.EqualValues(t, 0, stubRunner.PrepareCallsCount())
}
