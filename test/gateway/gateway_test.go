package gateway_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/gateway"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessFunctionRequest(t *testing.T) {
	// 1. Create a cancelable context so we can stop the server after the test
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := server.MonitoringOpts{
		AppName:    "gateway",
		AppVersion: "0.0.1",
		Env:        "LOCAL",
	}

	gateway_cfg := gateway.GatewayConfig{}

	// 2. Start the server in a goroutine (background)
	go func() {
		_ = gateway.Run(ctx, opts, gateway_cfg)
	}()

	// 3. Give the server a moment to start up
	time.Sleep(100 * time.Millisecond)

	// 4. Call the endpoint using a standard HTTP client
	url := "http://localhost:42069/my-function/some/path"
	resp, err := http.Post(url, "application/json", nil)

	require.NoError(t, err, "Failed to call gateway")
	defer resp.Body.Close()

	// 5. Read and check the response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	expected := "hi, you are calling function 'my-function' with endpoint 'some/path'"
	assert.Equal(t, expected, string(body))
}
