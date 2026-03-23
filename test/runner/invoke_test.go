package runner_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/guest"
	"github.com/Nesquiko/servermore/pkg/runner"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	testingguestconsts "github.com/Nesquiko/servermore/test/testing-guest/consts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeFunctionInstance_OKResponseFromTestingGuest(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 201
	const functionFilename = "201.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	ctx := t.Context()
	invokeResp, err := client.InvokeFunctionInstance(ctx, &runner.InvokeInstanceRequest{
		InstanceId: prepareResp.GetInstanceId(),
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathOK,
	})
	require.NoError(t, err)

	assert.EqualValues(t, 200, invokeResp.GetStatusCode())
	assert.Equal(t, testingguestconsts.HeaderJSON, invokeResp.GetHeaders()["content-type"])
	assert.Equal(t, []byte(testingguestconsts.BodyOK), invokeResp.GetBody())
}

func TestInvokeFunctionInstance_NotFoundPathReturnedByGuest(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 202
	const functionFilename = "202.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	notFoundPath := fmt.Sprintf("/not-found-%d", functionID)
	invokeResp, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: prepareResp.GetInstanceId(),
		Method:     http.MethodGet,
		Path:       notFoundPath,
	})
	require.NoError(t, err)

	assert.EqualValues(t, 404, invokeResp.GetStatusCode())
	assert.Equal(t, testingguestconsts.HeaderJSON, invokeResp.GetHeaders()["content-type"])
	assert.Equal(t, []byte(testingguestconsts.BodyNotFound), invokeResp.GetBody())
}

func TestInvokeFunctionInstance_ReturnsErrorWhenGuestReturnsRPCError(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 203
	const functionFilename = "203.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	invokeResp, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: prepareResp.GetInstanceId(),
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathError,
	})
	require.Error(t, err)
	assert.Nil(t, invokeResp)
	assert.ErrorContains(t, err, testingguestconsts.ErrorMessage)
}

func TestInvokeFunctionInstance_ReturnsErrorWhenGuestReturnsNilResponse(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 204
	const functionFilename = "204.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	invokeResp, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: prepareResp.GetInstanceId(),
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathNil,
	})
	require.Error(t, err)
	assert.Nil(t, invokeResp)
	assert.ErrorContains(t, err, guest.NilResponseErrorMsg)
}

func TestInvokeFunctionInstance_InvalidInstanceID(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	invokeResp, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: "not-a-uuid",
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathOK,
	})
	require.Error(t, err)
	assert.Nil(t, invokeResp)
	assert.ErrorContains(t, err, "invalid uuid instanceId")
}

func TestInvokeFunctionInstance_UnknownInstanceID(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	unknownID, err := uuid.NewV7()
	require.NoError(t, err)

	invokeResp, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: unknownID.String(),
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathOK,
	})
	require.Error(t, err)
	assert.Nil(t, invokeResp)
	assert.ErrorAs(t, err, &runner.UnknownInstanceErr)
}

func TestInvokeFunctionInstance_ConcurrentRequestsReuseSamePreparedInstance(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 205
	const functionFilename = "205.bin"
	const requestsCount = 10

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

	var wg sync.WaitGroup
	wg.Add(requestsCount)
	for i := range requestsCount {
		go func(i int) {
			defer wg.Done()
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
	wg.Wait()

	for i := range requestsCount {
		require.NoError(t, errs[i])
		require.NotNil(t, responses[i])
		assert.EqualValues(t, 200, responses[i].GetStatusCode())
		assert.Equal(t, testingguestconsts.HeaderJSON, responses[i].GetHeaders()["content-type"])
		assert.Equal(t, []byte(testingguestconsts.BodyOK), responses[i].GetBody())
	}
}

func TestInvokeFunctionInstance_AfterInstanceShutdownReturnsError(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 206
	const functionFilename = "206.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp.GetInstanceId())

	time.Sleep(TestInstanceShutdownAfter + 250*time.Millisecond)

	invokeResp, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: prepareResp.GetInstanceId(),
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathOK,
	})
	require.Error(t, err)
	assert.Nil(t, invokeResp)
	assert.ErrorAs(t, err, &runner.UnknownInstanceErr)
}

func TestPrepareFunctionInstance_SequentialPreparesCreateDistinctInstances(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	const functionID int64 = 207
	const functionFilename = "207.bin"

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	prepareResp1, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp1.GetInstanceId())

	prepareResp2, err := prepareFunctionInstance(t, client, functionID, functionPath)
	require.NoError(t, err)
	require.NotEmpty(t, prepareResp2.GetInstanceId())

	assert.NotEqual(t, prepareResp1.GetInstanceId(), prepareResp2.GetInstanceId())

	invokeResp1, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: prepareResp1.GetInstanceId(),
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathOK,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 200, invokeResp1.GetStatusCode())
	assert.Equal(t, testingguestconsts.HeaderJSON, invokeResp1.GetHeaders()["content-type"])
	assert.Equal(t, []byte(testingguestconsts.BodyOK), invokeResp1.GetBody())

	invokeResp2, err := client.InvokeFunctionInstance(t.Context(), &runner.InvokeInstanceRequest{
		InstanceId: prepareResp2.GetInstanceId(),
		Method:     http.MethodGet,
		Path:       testingguestconsts.PathOK,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 200, invokeResp2.GetStatusCode())
	assert.Equal(t, testingguestconsts.HeaderJSON, invokeResp2.GetHeaders()["content-type"])
	assert.Equal(t, []byte(testingguestconsts.BodyOK), invokeResp2.GetBody())
}
