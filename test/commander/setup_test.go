package commander_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var (
	HttpServerUrl   string
	GrcpServerUrl   string
	TestStorageRoot string
	DbFilePath      string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpPort, err := testutils.RandomFreePort()
	if err != nil {
		slog.Error("failed to allocate commander http port", "error", err)
		os.Exit(1)
	}

	grpcPort, err := testutils.RandomFreePort()
	if err != nil {
		slog.Error("failed to allocate commander grpc port", "error", err)
		os.Exit(1)
	}

	TestStorageRoot, err = testutils.SubdirInTempDir("commander")
	if err != nil {
		slog.Error("failed to create commander tmpDir", "error", err)
		os.Exit(1)
	}
	DbFilePath = filepath.Join(TestStorageRoot, "test-commander.db")

	config := commander.CommanderConfig{
		AppName:         "test-commander",
		Env:             "TEST",
		Host:            "localhost",
		HttpPort:        httpPort,
		GrpcPort:        grpcPort,
		DbURI:           DbFilePath,
		FuncStorageRoot: TestStorageRoot,
	}
	HttpServerUrl = fmt.Sprintf("http://%s:%s", config.Host, config.HttpPort)
	GrcpServerUrl = config.GrpcAddr()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- commander.Run(ctx, config)
	}()

	var eg errgroup.Group
	eg.Go(func() error {
		return testutils.WaitForHttpReady(
			ctx,
			"commander-http",
			HttpServerUrl+server.HeartbeatEndpoint,
		)
	})
	eg.Go(func() error {
		return testutils.WaitForGrpcReady(ctx, "commander-grpc", GrcpServerUrl)
	})

	errCh := make(chan error, 1)
	errCh <- eg.Wait()

	select {
	case err = <-runErrCh:
		if err != nil {
			slog.Error("commander exited before becoming ready", "error", err)
			os.Exit(1)
		}
		slog.Error("commander exited before becoming ready")
		os.Exit(1)
	case err = <-errCh:
		if err != nil {
			slog.Error("commander http/grpc server is not ready", "error", err)
			os.Exit(1)
		}
	}

	exitCode := m.Run()
	if err := os.RemoveAll(TestStorageRoot); err != nil {
		slog.Error("failed to remove temp dir", "dir", TestStorageRoot, "error", err)
	}

	os.Exit(exitCode)
}

func newCommanderClient(t *testing.T) commander.CommanderClient {
	t.Helper()

	monitoringOpts := server.MonitoringOpts{
		Env:             "TEST",
		AppName:         "commander-test-client",
		AdditionalAttrs: map[string]string{"test.name": t.Name()},
		Level:           slog.LevelDebug,
	}

	conn, err := server.LoggingGrpcClient(GrcpServerUrl, monitoringOpts)
	require.NoError(t, err)
	t.Cleanup(func() {
		server.Close(conn)
	})

	return commander.NewCommanderClient(conn)
}
