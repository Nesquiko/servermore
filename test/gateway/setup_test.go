package gateway_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/gateway"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
)

var (
	GatewayUrl    string
	StubCommander *testutils.StubCommander
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	port, err := testutils.RandomFreePort()
	assert.NoError(err)

	StubCommander = testutils.RunStubCommander(ctx)

	gatewayAddr := fmt.Sprintf("127.0.0.1:%s", port)
	config := gateway.GatewayConfig{
		Env:     server.TEST,
		AppName: "testing-gateway",
		Address: gatewayAddr,
	}

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- gateway.Run(ctx, config)
	}()

	GatewayUrl = "http://" + gatewayAddr
	readyErrCh := make(chan error, 1)
	go func() {
		readyErrCh <- testutils.WaitForHttpReady(
			ctx,
			"gateway",
			GatewayUrl+server.HeartbeatEndpoint,
		)
	}()

	select {
	case err = <-runErrCh:
		if err != nil {
			slog.Error("gateway exited before becoming ready", "error", err)
			os.Exit(1)
		}
		slog.Error("gateway exited before becoming ready")
		os.Exit(1)
	case err = <-readyErrCh:
		if err != nil {
			slog.Error("http gateway server not answering", "error", err)
			os.Exit(1)
		}
	}

	exitCode := m.Run()

	StubCommander.Close()
	os.Exit(exitCode)
}
