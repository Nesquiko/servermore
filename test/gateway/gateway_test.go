package gateway_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/gateway"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils" // Uprav import podľa tvojej štruktúry
	"github.com/stretchr/testify/assert"
)

func TestGatewayFullFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := testutils.RunStubRunner(ctx)
	defer runner.Close()
	runner.SetInvoke(&runnergrpc.InvokeInstanceResponse{
		StatusCode: 200,
		Body:       []byte("Hello from Runner!"),
		Headers:    map[string]string{"Content-Type": "text/plain"},
	}, nil)

	commanderStub := testutils.RunStubCommander(ctx)
	commanderStub.FixedRunnerAddr = runner.GrpcAddr()
	commanderStub.FixedInstanceID = "test-runner"
	defer commanderStub.Close()

	go func() {
		opts := server.MonitoringOpts{AppName: "gateway-test"}
		cfg := gateway.GatewayConfig{CommanderAddr: commanderStub.GrpcAddr()}
		_ = gateway.Run(ctx, opts, cfg)
	}()

	time.Sleep(200 * time.Millisecond) // wait for the gateway to start

	// HTTP request to the Gateway
	resp, err := http.Post("http://localhost:42069/my-function/some-path", "application/json", nil)
	assert.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Hello from Runner!", string(body))

	lastReq := runner.LastInvoke()
	assert.Equal(t, "test-runner", lastReq.InstanceId)
	assert.Equal(t, "/my-function/some-path", lastReq.Path)
}
