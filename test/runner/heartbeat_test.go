package runner_test

import (
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/runner"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	testingguestconsts "github.com/Nesquiko/servermore/test/testing-guest/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeartbeat_EmptyWhenNoInstancesRunning(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	heartbeatResp, err := client.Heartbeat(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, heartbeatResp.GetQueueDepths())
}

func TestHeartbeat_ReportsPreparedInstanceWithZeroQueueDepth(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 301
	const functionFilename = "301.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	heartbeatResp, err := client.Heartbeat(t.Context(), nil)
	require.NoError(t, err)

	queueDepths := heartbeatResp.GetQueueDepths()
	depth, ok := queueDepths[prepareResp.GetInstanceId()]
	require.True(t, ok)
	assert.EqualValues(t, 0, depth)
}

func TestHeartbeat_ReportsQueuedInvocationsForRunningInstance(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 302
	const functionFilename = "302.bin"
	const requestsCount = 50

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	responses := make([]*runner.InvokeInstanceResponse, requestsCount)
	errs := make([]error, requestsCount)

	startCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(requestsCount)
	for i := range requestsCount {
		go func(i int) {
			defer wg.Done()
			<-startCh
			responses[i], errs[i] = client.InvokeFunctionInstance(
				t.Context(),
				&runner.InvokeInstanceRequest{
					InstanceId: prepareResp.GetInstanceId(),
					Method:     http.MethodGet,
					Path:       testingguestconsts.PathOK,
				},
			)
		}(i)
	}
	close(startCh)

	deadline := time.Now().Add(2 * time.Second)
	seenQueued := false
	for time.Now().Before(deadline) {
		heartbeatResp, err := client.Heartbeat(t.Context(), nil)
		require.NoError(t, err)

		if depth, ok := heartbeatResp.GetQueueDepths()[prepareResp.GetInstanceId()]; ok &&
			depth > 0 {
			seenQueued = true
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()

	for i := range requestsCount {
		require.NoError(t, errs[i])
		require.NotNil(t, responses[i])
		assert.EqualValues(t, 200, responses[i].GetStatusCode())
	}

	assert.True(t, seenQueued)
}

func TestHeartbeat_DoesNotReportStoppedInstanceAfterShutdownTimeout(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 303
	const functionFilename = "303.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	time.Sleep(TestInstanceShutdownAfter + 250*time.Millisecond)

	heartbeatResp, err := client.Heartbeat(t.Context(), nil)
	require.NoError(t, err)
	assert.NotContains(t, heartbeatResp.GetQueueDepths(), prepareResp.GetInstanceId())
}
