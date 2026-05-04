package commander_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/caching"
	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/routing"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPollRunnerHeartbeats_CorrectRunnerUpdatesCache(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, cache, db := newRunnerCheckService(t)

	runner := testutils.RunCorrectStubRunner(ctx, &runnergrpc.HeartbeatResponse{
		QueueDepths: map[string]uint32{
			"fn-1-inst-a": 2,
			"fn-1-inst-b": 5,
		},
		CpuPercent:        37.5,
		UnusedMemoryBytes: 4096,
	})
	t.Cleanup(runner.Close)

	registerRunnerRow(t, ctx, db, runner.GrpcAddr())
	require.NoError(t, cache.SetInstance(ctx, "fn-1", "fn-1-inst-a", runner.GrpcAddr(), 0))
	require.NoError(t, cache.SetInstance(ctx, "fn-1", "fn-1-inst-b", runner.GrpcAddr(), 0))

	require.NoError(t, svc.PollRunnerHeartbeats(ctx, time.Now()))

	instances, err := cache.FunctionIdInstances(ctx, "fn-1")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{
		"fn-1-inst-a": 2,
		"fn-1-inst-b": 5,
	}, instances)

	reqs, err := cache.RequestsPerRunner(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{runner.GrpcAddr(): 7}, reqs)

	stats, err := cache.StatsPerRunner(ctx)
	require.NoError(t, err)
	assert.Equal(
		t,
		caching.ResourceMetrics{CpuPercent: 37.5, UnusedMemoryBytes: 4096},
		stats[runner.GrpcAddr()],
	)

	addr, err := cache.RunnerAddressOfInstance(ctx, "fn-1-inst-a")
	require.NoError(t, err)
	assert.Equal(t, runner.GrpcAddr(), addr)
	assert.EqualValues(t, 1, runner.HeartbeatCalls())
}

func TestPollRunnerHeartbeats_EmptyRunnerClearsCachedInstances(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, cache, db := newRunnerCheckService(t)

	runner := testutils.RunEmptyStubRunner(ctx)
	t.Cleanup(runner.Close)

	registerRunnerRow(t, ctx, db, runner.GrpcAddr())
	require.NoError(t, cache.SetInstance(ctx, "fn-2", "fn-2-inst-a", runner.GrpcAddr(), 4))

	require.NoError(t, svc.PollRunnerHeartbeats(ctx, time.Now()))

	instances, err := cache.FunctionIdInstances(ctx, "fn-2")
	require.NoError(t, err)
	assert.Empty(t, instances)

	reqs, err := cache.RequestsPerRunner(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{runner.GrpcAddr(): 0}, reqs)

	stats, err := cache.StatsPerRunner(ctx)
	require.NoError(t, err)
	assert.Equal(t, caching.ResourceMetrics{}, stats[runner.GrpcAddr()])

	_, err = cache.RunnerAddressOfInstance(ctx, "fn-2-inst-a")
	assert.Error(t, err)
	assert.EqualValues(t, 1, runner.HeartbeatCalls())
}

func TestPollRunnerHeartbeats_ErrorRunnerEvictsCache(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, cache, db := newRunnerCheckService(t)

	runner := testutils.RunErrorStubRunner(ctx, errors.New("runner unavailable"))
	t.Cleanup(runner.Close)

	registerRunnerRow(t, ctx, db, runner.GrpcAddr())
	require.NoError(t, cache.SetInstance(ctx, "fn-3", "fn-3-inst-a", runner.GrpcAddr(), 8))

	require.NoError(t, svc.PollRunnerHeartbeats(ctx, time.Now()))

	instances, err := cache.FunctionIdInstances(ctx, "fn-3")
	require.NoError(t, err)
	assert.Empty(t, instances)

	reqs, err := cache.RequestsPerRunner(ctx)
	require.NoError(t, err)
	assert.Empty(t, reqs)

	stats, err := cache.StatsPerRunner(ctx)
	require.NoError(t, err)
	assert.Empty(t, stats)

	_, err = cache.RunnerAddressOfInstance(ctx, "fn-3-inst-a")
	assert.Error(t, err)
	assert.EqualValues(t, 1, runner.HeartbeatCalls())
}

func TestPollRunnerHeartbeats_PollsAllRunners(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc, cache, db := newRunnerCheckService(t)

	correctRunner := testutils.RunCorrectStubRunner(ctx, &runnergrpc.HeartbeatResponse{
		QueueDepths: map[string]uint32{
			"fn-1-inst-a": 2,
		},
		CpuPercent:        50,
		UnusedMemoryBytes: 1024,
	})
	t.Cleanup(correctRunner.Close)

	emptyRunner := testutils.RunEmptyStubRunner(ctx)
	t.Cleanup(emptyRunner.Close)

	slowRunner := testutils.RunSlowStubRunner(ctx, 6*time.Second, &runnergrpc.HeartbeatResponse{
		QueueDepths: map[string]uint32{
			"fn-3-inst-a": 9,
		},
		CpuPercent:        80,
		UnusedMemoryBytes: 512,
	})
	t.Cleanup(slowRunner.Close)

	registerRunnerRow(t, ctx, db, correctRunner.GrpcAddr())
	registerRunnerRow(t, ctx, db, emptyRunner.GrpcAddr())
	registerRunnerRow(t, ctx, db, slowRunner.GrpcAddr())

	require.NoError(t, cache.SetInstance(ctx, "fn-1", "fn-1-inst-a", correctRunner.GrpcAddr(), 0))
	require.NoError(t, cache.SetInstance(ctx, "fn-2", "fn-2-inst-a", emptyRunner.GrpcAddr(), 4))
	require.NoError(t, cache.SetInstance(ctx, "fn-3", "fn-3-inst-a", slowRunner.GrpcAddr(), 8))

	require.NoError(t, svc.PollRunnerHeartbeats(ctx, time.Now()))

	assert.EqualValues(t, 1, correctRunner.HeartbeatCalls())
	assert.EqualValues(t, 1, emptyRunner.HeartbeatCalls())
	assert.EqualValues(t, 1, slowRunner.HeartbeatCalls())

	fn1Instances, err := cache.FunctionIdInstances(ctx, "fn-1")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"fn-1-inst-a": 2}, fn1Instances)

	fn2Instances, err := cache.FunctionIdInstances(ctx, "fn-2")
	require.NoError(t, err)
	assert.Empty(t, fn2Instances)

	fn3Instances, err := cache.FunctionIdInstances(ctx, "fn-3")
	require.NoError(t, err)
	assert.Empty(t, fn3Instances)

	reqs, err := cache.RequestsPerRunner(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{
		correctRunner.GrpcAddr(): 2,
		emptyRunner.GrpcAddr():   0,
	}, reqs)

	stats, err := cache.StatsPerRunner(ctx)
	require.NoError(t, err)
	assert.Equal(
		t,
		caching.ResourceMetrics{CpuPercent: 50, UnusedMemoryBytes: 1024},
		stats[correctRunner.GrpcAddr()],
	)
	assert.Equal(t, caching.ResourceMetrics{}, stats[emptyRunner.GrpcAddr()])
	_, ok := stats[slowRunner.GrpcAddr()]
	assert.False(t, ok)
}

func newRunnerCheckService(
	t *testing.T,
) (*commander.CommanderService, *caching.InMemoryCache, *commander.SQLiteCommanderDB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "commander.db")
	db, err := commander.NewSQLiteDB(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	funcStorageRoot := filepath.Join(t.TempDir(), "functions")
	funcStorage, err := commander.NewFSFunctionStorage(funcStorageRoot)
	require.NoError(t, err)

	cache := caching.NewInMemoryCache()
	router := routing.NewNaiveRouter(1, 1)
	svc := commander.NewCommanderService(
		db,
		funcStorage,
		cache,
		router,
		commander.CommanderServiceConfig{
			RunnerClientOpts: server.MonitoringOpts{Env: "TEST", AppName: "runner-check-test"},
		},
	)

	return svc, cache, db
}

func registerRunnerRow(
	t *testing.T,
	ctx context.Context,
	db *commander.SQLiteCommanderDB,
	addr string,
) {
	t.Helper()

	_, err := db.CreateRunner(ctx, addr)
	require.NoError(t, err)
}
