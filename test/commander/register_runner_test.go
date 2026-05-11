package commander_test

import (
	"database/sql"
	"sync"
	"testing"

	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterRunner_PersistsNewRunnerAfterSuccessfulHeartbeat(t *testing.T) {
	t.Parallel()

	client := newCommanderClient(t)
	stubRunner := testutils.RunStubRunner(t.Context())
	t.Cleanup(func() {
		stubRunner.Close()
	})

	var runnerId *int64

	t.Run("persists new runner", func(t *testing.T) {
		registerResp, err := client.RegisterRunner(
			t.Context(),
			&commandergrpc.RegisterRunnerRequest{
				Addr: stubRunner.GrpcAddr(),
			},
		)
		require.NoError(t, err)
		assert.NotZero(t, registerResp.GetRunnerId())
		runnerId = new(registerResp.GetRunnerId())

		runnerRow, err := TestQueries.RunnerByAddr(t.Context(), stubRunner.GrpcAddr())
		require.NoError(t, err)
		assert.Equal(t, stubRunner.GrpcAddr(), runnerRow.Addr)
		assert.Equal(t, registerResp.GetRunnerId(), runnerRow.ID)
	})

	t.Run("returns existing runner for known address", func(t *testing.T) {
		registerResp, err := client.RegisterRunner(
			t.Context(),
			&commandergrpc.RegisterRunnerRequest{
				Addr: stubRunner.GrpcAddr(),
			},
		)
		require.NoError(t, err)
		require.NotNil(t, runnerId)
		assert.Equal(t, *runnerId, registerResp.GetRunnerId())

		runnerRow, err := TestQueries.RunnerByAddr(t.Context(), stubRunner.GrpcAddr())
		require.NoError(t, err)
		assert.Equal(t, *runnerId, runnerRow.ID)
	})
}

func TestRegisterRunner_FailsWhenRunnerHeartbeatFails(t *testing.T) {
	t.Parallel()

	client := newCommanderClient(t)
	port, err := testutils.RandomFreePort()
	require.NoError(t, err)
	runnerAddr := "127.0.0.1:" + port

	registerResp, err := client.RegisterRunner(t.Context(), &commandergrpc.RegisterRunnerRequest{
		Addr: runnerAddr,
	})
	require.Error(t, err)
	assert.Nil(t, registerResp)
	assert.ErrorContains(t, err, "registering runner failed")

	_, err = TestQueries.RunnerByAddr(t.Context(), runnerAddr)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestRegisterRunner_DifferentAddressesCreateDistinctRunners(t *testing.T) {
	t.Parallel()

	client := newCommanderClient(t)
	stubRunner1 := testutils.RunStubRunner(t.Context())
	stubRunner2 := testutils.RunStubRunner(t.Context())
	t.Cleanup(func() {
		stubRunner1.Close()
		stubRunner2.Close()
	})

	registerResp1, err := client.RegisterRunner(t.Context(), &commandergrpc.RegisterRunnerRequest{
		Addr: stubRunner1.GrpcAddr(),
	})
	require.NoError(t, err)
	assert.NotZero(t, registerResp1.GetRunnerId())

	registerResp2, err := client.RegisterRunner(t.Context(), &commandergrpc.RegisterRunnerRequest{
		Addr: stubRunner2.GrpcAddr(),
	})
	require.NoError(t, err)
	assert.NotZero(t, registerResp2.GetRunnerId())

	assert.NotEqual(t, registerResp1.GetRunnerId(), registerResp2.GetRunnerId())

	runnerRow1, err := TestQueries.RunnerByAddr(t.Context(), stubRunner1.GrpcAddr())
	require.NoError(t, err)
	assert.Equal(t, stubRunner1.GrpcAddr(), runnerRow1.Addr)
	assert.Equal(t, registerResp1.GetRunnerId(), runnerRow1.ID)

	runnerRow2, err := TestQueries.RunnerByAddr(t.Context(), stubRunner2.GrpcAddr())
	require.NoError(t, err)
	assert.Equal(t, stubRunner2.GrpcAddr(), runnerRow2.Addr)
	assert.Equal(t, registerResp2.GetRunnerId(), runnerRow2.ID)
}

func TestRegisterRunner_ConcurrentCallsForSameAddressReturnOneRunner(t *testing.T) {
	t.Parallel()

	client := newCommanderClient(t)
	stubRunner := testutils.RunStubRunner(t.Context())
	t.Cleanup(func() {
		stubRunner.Close()
	})

	const requestsCount = 10

	responses := make([]*commandergrpc.RegisterRunnerResponse, requestsCount)
	errs := make([]error, requestsCount)

	var wg sync.WaitGroup
	wg.Add(requestsCount)
	for i := range requestsCount {
		go func(i int) {
			defer wg.Done()
			responses[i], errs[i] = client.RegisterRunner(
				t.Context(),
				&commandergrpc.RegisterRunnerRequest{
					Addr: stubRunner.GrpcAddr(),
				},
			)
		}(i)
	}
	wg.Wait()

	var successRunnerID int64
	var successCount int
	for i := range requestsCount {
		if errs[i] != nil {
			assert.Nil(t, responses[i])
			assert.ErrorContains(t, errs[i], "UNIQUE constraint failed: runners.addr")
			continue
		}

		require.NotNil(t, responses[i])
		assert.NotZero(t, responses[i].GetRunnerId())
		if successCount == 0 {
			successRunnerID = responses[i].GetRunnerId()
		} else {
			assert.Equal(t, successRunnerID, responses[i].GetRunnerId())
		}
		successCount++
	}
	require.Greater(t, successCount, 0)

	runnerRow, err := TestQueries.RunnerByAddr(t.Context(), stubRunner.GrpcAddr())
	require.NoError(t, err)
	assert.Equal(t, stubRunner.GrpcAddr(), runnerRow.Addr)
	assert.Equal(t, successRunnerID, runnerRow.ID)
}

func TestRegisterRunner_ReRegistrationAfterInitialFailureSucceeds(t *testing.T) {
	t.Parallel()

	client := newCommanderClient(t)
	port, err := testutils.RandomFreePort()
	require.NoError(t, err)
	runnerAddr := "127.0.0.1:" + port

	registerResp, err := client.RegisterRunner(t.Context(), &commandergrpc.RegisterRunnerRequest{
		Addr: runnerAddr,
	})
	require.Error(t, err)
	assert.Nil(t, registerResp)
	assert.ErrorContains(t, err, "heartbeat failed")

	_, err = TestQueries.RunnerByAddr(t.Context(), runnerAddr)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	stubRunner := testutils.RunStubRunnerOnPort(t.Context(), port)
	t.Cleanup(func() {
		stubRunner.Close()
	})

	registerResp, err = client.RegisterRunner(t.Context(), &commandergrpc.RegisterRunnerRequest{
		Addr: runnerAddr,
	})
	require.NoError(t, err)
	assert.NotZero(t, registerResp.GetRunnerId())

	runnerRow, err := TestQueries.RunnerByAddr(t.Context(), runnerAddr)
	require.NoError(t, err)
	assert.Equal(t, runnerAddr, runnerRow.Addr)
	assert.Equal(t, registerResp.GetRunnerId(), runnerRow.ID)
}
