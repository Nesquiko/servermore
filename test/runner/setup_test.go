package runner_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/runner"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
)

var (
	ServerUrl             string
	TestRunnerStorageRoot string
	StubCommander         *testutils.StubCommander
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
		Env:                   "TEST",
		AppName:               "testing-runner",
		Addr:                  ServerUrl,
		CommanderAddress:      StubCommander.Addr(),
		InstanceShutdownAfter: 1 * time.Second,
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
	if err := os.RemoveAll(TestRunnerStorageRoot); err != nil {
		slog.Error("failed to remove temp dir", "dir", TestRunnerStorageRoot, "error", err)
	}

	os.Exit(exitCode)
}
