package commander_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	commanderapi "github.com/Nesquiko/servermore/pkg/api/commander"
	"github.com/Nesquiko/servermore/pkg/caching"
	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	testqueries "github.com/Nesquiko/servermore/test/test_utils/queries.gen"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var (
	HttpServerUrl     string
	GrcpServerUrl     string
	TestStorageRoot   string
	DbFilePath        string
	TestQueries       testqueries.Querier
	TestCache         *caching.InMemoryCache
	TestCommanderConf commander.CommanderConfig
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

	TestCommanderConf = commander.CommanderConfig{
		AppName:                     "test-commander",
		Env:                         "TEST",
		Host:                        "localhost",
		HttpPort:                    httpPort,
		GrpcPort:                    grpcPort,
		DbURI:                       DbFilePath,
		FuncStorageRoot:             TestStorageRoot,
		RunnerHeartbeatPoll:         250 * time.Millisecond,
		RunnerOverloadedQueueSize:   5,
		InstanceOverloadedQueueSize: 2,
	}
	HttpServerUrl = fmt.Sprintf("http://%s:%s", TestCommanderConf.Host, TestCommanderConf.HttpPort)
	GrcpServerUrl = TestCommanderConf.GrpcAddr()

	TestCache = caching.NewInMemoryCache()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- commander.Run(ctx, TestCache, TestCommanderConf)
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

	TestQueries, err = testutils.OpenTestDB(ctx, DbFilePath)
	if err != nil {
		slog.Error("failed to create test commander queries", "error", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	if err := os.RemoveAll(filepath.Dir(TestStorageRoot)); err != nil {
		slog.Error("failed to remove temp dir", "dir", filepath.Dir(TestStorageRoot), "error", err)
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

func submitFunction(
	t *testing.T,
	name string,
	filename string,
	binaryBytes []byte,
) commanderapi.Function {
	t.Helper()

	bodyFile, contentType := createFunctionMultipartBodyFromBytes(
		t,
		name,
		filename,
		binaryBytes,
	)
	defer server.Close(bodyFile)

	req, err := http.NewRequest(http.MethodPost, HttpServerUrl+"/functions", bodyFile)
	require.NoError(t, err, "create request")
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "send request")
	defer server.Close(resp.Body)

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var created commanderapi.Function
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created), "decode response")
	require.NotZero(t, created.Id)

	return created
}
