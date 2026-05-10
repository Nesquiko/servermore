package gateway_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"

	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/require"
)

var gatewayTestMu sync.Mutex

func TestGatewayRootPathReturnsNotFound(t *testing.T) {
	t.Parallel()

	status, _, _ := doGatewayRequest(t, http.MethodGet, "/", nil, nil)
	require.Equal(t, http.StatusNotFound, status)
}

func TestGatewayForwardsFunctionRequestToRunner(t *testing.T) {
	t.Parallel()

	runner := testutils.RunStubRunner(t.Context())
	defer runner.Close()

	gatewayTestMu.Lock()
	defer gatewayTestMu.Unlock()
	defer StubCommander.ClearRouteConfig()

	runner.SetInvoke(&runnergrpc.InvokeInstanceResponse{StatusCode: http.StatusOK}, nil)
	functionID := testutils.AddRandomPart("gateway-forward")
	instanceID := testutils.AddRandomPart("gateway-instance")
	StubCommander.SetRouteResponse(runner.GrpcAddr(), instanceID)

	requestBody := []byte("gateway-body")
	status, _, _ := doGatewayRequest(
		t,
		http.MethodPost,
		"/"+functionID+"/nested/path",
		requestBody,
		map[string]string{"X-Test-Header": "gateway"},
	)
	require.Equal(t, http.StatusOK, status)

	lastInvoke := runner.LastInvoke()
	require.NotNil(t, lastInvoke)
	require.Equal(t, instanceID, lastInvoke.GetInstanceId())
	require.Equal(t, http.MethodPost, lastInvoke.GetMethod())
	require.Equal(t, "/nested/path", lastInvoke.GetPath())
	require.Equal(t, requestBody, lastInvoke.GetBody())
	require.Equal(t, "gateway", lastInvoke.GetHeaders()["X-Test-Header"])
}

func TestGatewayReturnsRunnerResponseToClient(t *testing.T) {
	t.Parallel()

	runner := testutils.RunStubRunner(t.Context())
	defer runner.Close()

	gatewayTestMu.Lock()
	defer gatewayTestMu.Unlock()
	defer StubCommander.ClearRouteConfig()

	runner.SetInvoke(
		&runnergrpc.InvokeInstanceResponse{
			StatusCode: http.StatusCreated,
			Headers: map[string]string{
				"Content-Type":    "text/plain",
				"X-Runner-Header": "runner-value",
			},
			Body: []byte("runner-response"),
		},
		nil,
	)
	functionID := testutils.AddRandomPart("gateway-response")
	StubCommander.SetRouteResponse(runner.GrpcAddr(), testutils.AddRandomPart("gateway-instance"))

	status, headers, respBody := doGatewayRequest(
		t,
		http.MethodGet,
		"/"+functionID+"/response",
		nil,
		nil,
	)
	require.Equal(t, http.StatusCreated, status)
	require.Equal(t, "runner-value", headers.Get("X-Runner-Header"))
	require.Equal(t, "text/plain", headers.Get("Content-Type"))
	require.Equal(t, []byte("runner-response"), respBody)
}

func TestGatewayRejectsOversizedRequestBody(t *testing.T) {
	t.Parallel()

	runner := testutils.RunStubRunner(t.Context())
	defer runner.Close()

	gatewayTestMu.Lock()
	defer gatewayTestMu.Unlock()
	defer StubCommander.ClearRouteConfig()

	runner.SetInvoke(&runnergrpc.InvokeInstanceResponse{StatusCode: http.StatusOK}, nil)
	functionID := testutils.AddRandomPart("gateway-oversized")
	StubCommander.SetRouteResponse(runner.GrpcAddr(), testutils.AddRandomPart("gateway-instance"))

	body := bytes.Repeat([]byte("a"), server.MaxBytes+1)
	status, _, respBody := doGatewayRequest(
		t,
		http.MethodPost,
		"/"+functionID+"/upload",
		body,
		nil,
	)
	require.Equal(t, http.StatusRequestEntityTooLarge, status)
	require.Nil(t, runner.LastInvoke())
	require.Contains(t, string(respBody), "request.too.large")
}

func TestGatewaySurfacesRunnerInvokeError(t *testing.T) {
	t.Parallel()

	runner := testutils.RunStubRunner(t.Context())
	defer runner.Close()

	gatewayTestMu.Lock()
	defer gatewayTestMu.Unlock()
	defer StubCommander.ClearRouteConfig()

	runner.SetInvoke(nil, errors.New("runner invoke failed"))
	functionID := testutils.AddRandomPart("gateway-invoke-error")
	StubCommander.SetRouteResponse(runner.GrpcAddr(), testutils.AddRandomPart("gateway-instance"))

	status, _, respBody := doGatewayRequest(
		t,
		http.MethodGet,
		"/"+functionID+"/invoke-error",
		nil,
		nil,
	)
	require.Equal(t, http.StatusInternalServerError, status)
	require.NotNil(t, runner.LastInvoke())
	require.Contains(t, string(respBody), "internal.server.error")
}

func TestGatewaySurfacesCommanderRouteError(t *testing.T) {
	t.Parallel()

	gatewayTestMu.Lock()
	StubCommander.SetRouteError(errors.New("route failed"))
	defer gatewayTestMu.Unlock()
	defer StubCommander.ClearRouteConfig()

	status, _, respBody := doGatewayRequest(
		t,
		http.MethodGet,
		"/"+testutils.AddRandomPart("gateway-route-error")+"/route-error",
		nil,
		nil,
	)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, string(respBody), "internal.server.error")
}

func doGatewayRequest(
	t *testing.T,
	method string,
	path string,
	body []byte,
	headers map[string]string,
) (int, http.Header, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, GatewayUrl+path, reader)
	require.NoError(t, err)

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer server.Close(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, resp.Header, respBody
}
