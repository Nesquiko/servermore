package gateway

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/Nesquiko/servermore/pkg/assert"
	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	"google.golang.org/grpc"
)

const FunctionIdPathParam = "functionId"

type gatewayHandler struct {
	commanderClient commandergrpc.CommanderClient

	runnersMu sync.RWMutex
	runners   map[string]*grpc.ClientConn

	runnerMonitoringOpts server.MonitoringOpts
}

func (h *gatewayHandler) processFunctionRequest(w http.ResponseWriter, r *http.Request) {
	functionId := chi.URLParam(r, FunctionIdPathParam)

	if functionId == "" {
		server.EncodeError(w, r, server.Error{
			Cause:  fmt.Errorf("path parameter %q is required", FunctionIdPathParam),
			Code:   server.InvalidRequestCode,
			Detail: fmt.Sprintf("Path parameter %q is required", FunctionIdPathParam),
			Status: http.StatusBadRequest,
			Title:  server.InvalidRequestTitle,
		})
		return
	}

	routeResp, err := h.commanderClient.RouteFunction(
		r.Context(),
		&commandergrpc.RouteFunctionRequest{
			FunctionId: functionId,
		},
	)
	if err != nil {
		server.InternalServerError(w, r, fmt.Errorf("route call failed: %w", err))
		return
	}

	runnerConn, err := h.runnerConn(routeResp.RunnerAddr)
	if err != nil {
		server.InternalServerError(w, r, err)
		return
	}

	runnerClient := runnergrpc.NewRunnerClient(runnerConn)

	r.Body = http.MaxBytesReader(w, r.Body, int64(server.MaxBytes))
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if err.Error() == server.LargeBodyErrorStr {
			server.EncodeError(w, r, server.Error{
				Cause:  err,
				Code:   "request.too.large",
				Title:  "Request body too large",
				Detail: fmt.Sprintf("Request body must be <= %d bytes", server.MaxBytes),
				Status: http.StatusRequestEntityTooLarge,
			})
			return
		}
		server.InternalServerError(w, r, err)
		return
	}

	invokeResp, err := runnerClient.InvokeFunctionInstance(
		r.Context(),
		&runnergrpc.InvokeInstanceRequest{
			InstanceId: routeResp.InstanceId,
			Method:     r.Method,
			Path:       forwardedPath(r.URL.Path, functionId),
			Headers:    flattenHeaders(r.Header),
			Body:       body,
		},
	)
	if err != nil {
		server.InternalServerError(w, r, fmt.Errorf("invoke call failed: %w", err))
		return
	}

	for k, v := range invokeResp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(int(invokeResp.StatusCode))
	if _, err := w.Write(invokeResp.Body); err != nil {
		server.SetAPIError(r, server.NewApiError(r, server.Error{
			Cause:  err,
			Code:   "internal.server.error",
			Detail: "Failed to write response body",
			Status: http.StatusInternalServerError,
			Title:  "Internal server error",
		}))
	}
}

func (h *gatewayHandler) runnerConn(addr string) (*grpc.ClientConn, error) {
	// first optimistic check
	h.runnersMu.RLock()
	conn, ok := h.runners[addr]
	h.runnersMu.RUnlock()
	if ok {
		return conn, nil
	}

	// second check with the mutex write locked
	h.runnersMu.Lock()
	defer h.runnersMu.Unlock()

	if conn, ok := h.runners[addr]; ok {
		return conn, nil
	}

	newConn, err := server.LoggingGrpcClient(addr, h.runnerMonitoringOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize grpc client connection: %w", err)
	}

	h.runners[addr] = newConn
	return newConn, nil
}

func (h *gatewayHandler) Close() {
	h.runnersMu.Lock()
	defer h.runnersMu.Unlock()

	for addr, conn := range h.runners {
		delete(h.runners, addr)
		server.Close(conn)
	}
}

func forwardedPath(path string, functionId string) string {
	assert.That(functionId != "", "function id was empty string")

	trimmed := strings.TrimPrefix(path, "/"+functionId)
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func flattenHeaders(h http.Header) map[string]string {
	flat := make(map[string]string)
	for k, v := range h {
		if len(v) > 0 {
			flat[k] = v[0]
		}
	}
	return flat
}
