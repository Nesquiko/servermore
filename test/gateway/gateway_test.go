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

	// 1. Spusti Runner a nastav mu, čo má vrátiť na HTTP volanie
	runner := testutils.RunStubRunner(ctx)
	defer runner.Close()
	runner.SetInvoke(&runnergrpc.InvokeInstanceResponse{
		StatusCode: 200,
		Body:       []byte("Hello from Runner!"),
		Headers:    map[string]string{"Content-Type": "text/plain"},
	}, nil)

	// 2. Spusti Commander a povedz mu, že má posielať requesty na ten Runner
	commanderStub := testutils.RunStubCommander(ctx)
	commanderStub.FixedRunnerAddr = runner.GrpcAddr()
	commanderStub.FixedInstanceID = "instance-abc"
	defer commanderStub.Close()

	// 3. Spusti Gateway (v gorutine)
	go func() {
		opts := server.MonitoringOpts{AppName: "gateway-test"}
		cfg := gateway.GatewayConfig{CommanderAddr: commanderStub.GrpcAddr()}
		_ = gateway.Run(ctx, opts, cfg)
	}()

	time.Sleep(200 * time.Millisecond) // Počkaj na štart

	// 4. Pošli HTTP request na Gateway
	resp, err := http.Post("http://localhost:42069/my-function/some-path", "application/json", nil)
	assert.NoError(t, err)
	defer resp.Body.Close()

	// 5. Overenie
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Hello from Runner!", string(body))

	// Over, či Gateway poslala správne InstanceID a cestu do gRPC
	lastReq := runner.LastInvoke()
	assert.Equal(t, "instance-abc", lastReq.InstanceId)
	assert.Equal(t, "/my-function/some-path", lastReq.Path)
}
