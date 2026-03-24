package runner_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Nesquiko/servermore/pkg/runner"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareFunctionInstance_DownloadsBinaryToRunnerStorageRoot(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	functionID := testutils.AddRandomPart("101")
	functionFilename := functionID

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	expectedRunnerPath := filepath.Join(TestRunnerStorageRoot, functionPath)
	require.NoError(t, testutils.DeleteIfExists(expectedRunnerPath))
	var firstInstanceID string

	t.Run("downloads binary", func(t *testing.T) {
		resp, err := prepareFunctionInstance(t, client, functionID, functionPath)
		require.NoError(t, err)

		firstInstanceID = resp.GetInstanceId()
		assert.NotEmpty(t, resp.GetInstanceId())
		assert.True(t, resp.GetDownloaded())
		assert.FileExists(t, expectedRunnerPath)
	})

	t.Run("reuses downloaded binary", func(t *testing.T) {
		statsBefore, err := os.Stat(expectedRunnerPath)
		require.NoError(t, err)

		resp, err := prepareFunctionInstance(t, client, functionID, functionPath)
		require.NoError(t, err)

		statsAfter, err := os.Stat(expectedRunnerPath)
		require.NoError(t, err)

		assert.NotEmpty(t, resp.GetInstanceId())
		assert.NotEqual(t, firstInstanceID, resp.GetInstanceId())
		assert.False(t, resp.GetDownloaded())
		assert.True(t, statsAfter.ModTime().Equal(statsBefore.ModTime()))
	})
}

func TestPrepareFunctionInstance_ConcurrentRequestsShareOneDownload(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	functionID := testutils.AddRandomPart("102")
	functionFilename := functionID

	binaryPath, err := filepath.Abs(testutils.TestingBinaryPath)
	require.NoError(t, err)

	StubCommander.DeleteFile(functionFilename)
	StubCommander.SymlinkFile(binaryPath, functionFilename)
	functionPath := StubCommander.PathFor(functionFilename)

	expectedRunnerPath := filepath.Join(TestRunnerStorageRoot, functionPath)
	require.NoError(t, testutils.DeleteIfExists(expectedRunnerPath))

	requestsCount := 10
	responses := make([]*runner.PrepareInstanceResponse, requestsCount)
	errs := make([]error, requestsCount)

	var wg sync.WaitGroup
	wg.Add(requestsCount)
	for i := range requestsCount {
		go func(i int) {
			defer wg.Done()
			responses[i], errs[i] = prepareFunctionInstance(t, client, functionID, functionPath)
		}(i)
	}
	wg.Wait()

	for i := range requestsCount {
		require.NoError(t, errs[i])
		require.NotNil(t, responses[i])
	}

	instanceID := responses[0].GetInstanceId()
	downloadedCount := 0
	for i := range requestsCount {
		assert.NotEmpty(t, responses[i].GetInstanceId())
		assert.Equal(t, instanceID, responses[i].GetInstanceId())
		if responses[i].GetDownloaded() {
			downloadedCount++
		}
	}

	assert.Equal(t, 1, downloadedCount)
	assert.FileExists(t, expectedRunnerPath)
}

func TestPrepareFunctionInstance_ConcurentErrorsWhenDownloadFails(t *testing.T) {
	t.Parallel()

	client := newRunnerClient(t)

	functionID := testutils.AddRandomPart("103")
	functionFilename := functionID

	functionPath := StubCommander.PathFor(functionFilename)
	expectedRunnerPath := filepath.Join(TestRunnerStorageRoot, functionPath)
	require.NoError(t, testutils.DeleteIfExists(expectedRunnerPath))

	downloadErr := errors.New("stub commander forced download failure")
	StubCommander.MarkPathToError(functionFilename, downloadErr)

	requestsCount := 10
	responses := make([]*runner.PrepareInstanceResponse, requestsCount)
	errs := make([]error, requestsCount)

	var wg sync.WaitGroup
	wg.Add(requestsCount)
	for i := range requestsCount {
		go func(i int) {
			defer wg.Done()
			responses[i], errs[i] = prepareFunctionInstance(t, client, functionID, functionPath)
		}(i)
	}
	wg.Wait()

	for i := range requestsCount {
		require.Error(t, errs[i])
		assert.Nil(t, responses[i])
		assert.ErrorAs(
			t,
			errs[i],
			&downloadErr,
			"commander errored with different than expected error %v",
			errs[i],
		)
	}

	assert.NoFileExists(t, expectedRunnerPath)
}
