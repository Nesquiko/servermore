package commander_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
)

var (
	ServerUrl       string
	TestStorageRoot string
	DbFilePath      string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	port, err := testutils.RandomFreePort()
	if err != nil {
		slog.Error("failed to allocate test port", "error", err)
		os.Exit(1)
	}

	TestStorageRoot, err = testutils.SubdirInTempDir("commander")
	if err != nil {
		slog.Error("failed to create commander tmpDir", "error", err)
		os.Exit(1)
	}
	DbFilePath = filepath.Join(TestStorageRoot, "test-commander.db")

	config := commander.CommanderHTTPServerConfig{
		AppName:         "test-commander",
		CommitHash:      "test",
		Env:             "TEST",
		Host:            "localhost",
		Port:            port,
		BaseURL:         "",
		DbURI:           DbFilePath,
		FuncStorageRoot: TestStorageRoot,
	}
	ServerUrl = fmt.Sprintf("http://%s:%s", config.Host, config.Port)

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- commander.Run(ctx, config)
	}()

	readyErrCh := make(chan error, 1)
	go func() {
		readyErrCh <- testutils.WaitForHttpReady(ctx, 1*time.Second, 100*time.Millisecond, ServerUrl+server.HeartbeatEndpoint)
	}()

	select {
	case err = <-runErrCh:
		if err != nil {
			slog.Error("commander exited before becoming ready", "error", err)
			os.Exit(1)
		}
		slog.Error("commander exited before becoming ready")
		os.Exit(1)
	case err = <-readyErrCh:
		if err != nil {
			slog.Error("ready endpoint not answering", "error", err)
			os.Exit(1)
		}
	}

	exitCode := m.Run()
	if err := os.RemoveAll(TestStorageRoot); err != nil {
		slog.Error("failed to remove temp dir", "dir", TestStorageRoot, "error", err)
	}

	os.Exit(exitCode)
}
