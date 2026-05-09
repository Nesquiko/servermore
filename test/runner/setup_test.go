package runner_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/runner"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/require"
)

var (
	ServerUrl                 string
	TestRunnerStorageRoot     string
	StubCommander             *testutils.StubCommander
	TestInstanceShutdownAfter time.Duration = 2 * time.Second
	TestInstanceGracePeriod   time.Duration = 500 * time.Millisecond
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	port, err := testutils.RandomFreePort()
	assert.NoError(err)

	TestRunnerStorageRoot, err = testutils.SubdirInTempDir("runner")
	assert.NoError(err)

	StubCommander = testutils.RunStubCommander(ctx)

	ServerUrl = fmt.Sprintf("127.0.0.1:%s", port)
	config := runner.RunnerConfig{
		Env:                   server.TEST,
		AppName:               "testing-runner",
		Addr:                  ServerUrl,
		CommanderHost:         StubCommander.Host(),
		CommanderGrpcPort:     StubCommander.GrpcPort(),
		CommanderHttpPort:     StubCommander.HttpPort(),
		InstanceShutdownAfter: TestInstanceShutdownAfter,
		InstanceGracePeriod:   TestInstanceGracePeriod,
		FuncStorageRoot:       TestRunnerStorageRoot,
	}

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- runner.Run(ctx, config)
	}()

	readyErrCh := make(chan error, 1)
	go func() {
		readyErrCh <- testutils.WaitForGrpcReady(ctx, "runner", ServerUrl)
	}()

	select {
	case err = <-runErrCh:
		if err != nil {
			slog.Error("runner exited before becoming ready", "error", err)
			os.Exit(1)
		}
		slog.Error("runner exited before becoming ready")
		os.Exit(1)
	case err = <-readyErrCh:
		if err != nil {
			slog.Error("grpc server not answering", "error", err)
			os.Exit(1)
		}
	}

	exitCode := m.Run()

	StubCommander.Close()
	if err := os.RemoveAll(filepath.Dir(TestRunnerStorageRoot)); err != nil {
		slog.Error(
			"failed to remove temp dir",
			"dir", filepath.Dir(TestRunnerStorageRoot),
			"error", err,
		)
	}

	os.Exit(exitCode)
}

func newRunnerClient(t *testing.T) runnergrpc.RunnerClient {
	t.Helper()
	return newRunnerClientWithLog(t, slog.LevelInfo)
}

func newRunnerClientWithLog(t *testing.T, logLevel slog.Level) runnergrpc.RunnerClient {
	t.Helper()

	monitoringOpts := server.MonitoringOpts{
		Env:             server.TEST,
		AppName:         "runner-test-client",
		AdditionalAttrs: map[string]string{"test.name": t.Name()},
		Level:           logLevel,
	}

	conn, err := server.LoggingGrpcClient(ServerUrl, monitoringOpts)
	require.NoError(t, err)
	t.Cleanup(func() {
		server.Close(conn)
	})

	return runnergrpc.NewRunnerClient(conn)
}

func prepareFunctionInstance(
	t *testing.T,
	client runnergrpc.RunnerClient,
	functionID string,
	functionPath string,
) (*runnergrpc.PrepareInstanceResponse, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	return client.PrepareFunctionInstance(ctx, &runnergrpc.PrepareInstanceRequest{
		FunctionId:   functionID,
		FunctionPath: functionPath,
	})
}
