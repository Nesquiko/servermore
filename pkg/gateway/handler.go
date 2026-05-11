package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Nesquiko/servermore/pkg/assert"
	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	grpc "google.golang.org/grpc"
)

const FunctionIdPathParam = "functionId"

type gatewayHandler struct {
	commanderClient commandergrpc.CommanderClient

	runnersMu sync.RWMutex
	runners   map[string]*grpc.ClientConn

	runnerMonitoringOpts server.MonitoringOpts
}

func (h *gatewayHandler) processFunctionRequest(w http.ResponseWriter, r *http.Request) {
	meta := &server.GatewayFunctionRequestMeta{
		RequestMethod: r.Method,
		RequestPath:   r.URL.Path,
	}
	server.SetGatewayFunctionRequestMeta(r.Context(), meta)

	functionId := chi.URLParam(r, FunctionIdPathParam)
	meta.FunctionID = functionId

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

	routeStart := time.Now()
	routeResp, err := h.commanderClient.RouteFunction(
		r.Context(),
		&commandergrpc.RouteFunctionRequest{
			FunctionId: functionId,
		},
	)
	if err != nil {
		meta.RouteTook = time.Since(routeStart)
		server.InternalServerError(w, r, fmt.Errorf("route call failed: %w", err))
		return
	}
	meta.RouteTook = time.Since(routeStart)
	meta.RunnerAddr = routeResp.RunnerAddr
	meta.InstanceID = routeResp.InstanceId

	runnerConn, reused, err := h.runnerConn(r.Context(), routeResp.RunnerAddr)
	if err != nil {
		server.InternalServerError(w, r, err)
		return
	}
	meta.RunnerConnReused = reused

	runnerClient := runnergrpc.NewRunnerClient(runnerConn)

	meta.HeadersCount = len(r.Header)
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
	meta.RequestBodyBytes = len(body)

	invokeRequestPath := forwardedPath(r.URL.Path, functionId)
	meta.ForwardedPath = invokeRequestPath

	invokeStart := time.Now()
	invokeResp, err := runnerClient.InvokeFunctionInstance(
		r.Context(),
		&runnergrpc.InvokeInstanceRequest{
			InstanceId: routeResp.InstanceId,
			Method:     r.Method,
			Path:       invokeRequestPath,
			Headers:    flattenHeaders(r.Header),
			Body:       body,
		},
	)
	meta.InvokeTook = time.Since(invokeStart)
	if err != nil {
		server.InternalServerError(w, r, fmt.Errorf("invoke call failed: %w", err))
		return
	}
	meta.ResponseStatusCode = int(invokeResp.StatusCode)
	meta.ResponseBodyBytes = len(invokeResp.Body)

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

func (h *gatewayHandler) runnerConn(
	ctx context.Context,
	addr string,
) (*grpc.ClientConn, bool, error) {
	// first optimistic check
	h.runnersMu.RLock()
	conn, ok := h.runners[addr]
	h.runnersMu.RUnlock()
	if ok {
		return conn, true, nil
	}

	// second check with the mutex write locked
	h.runnersMu.Lock()
	defer h.runnersMu.Unlock()

	if conn, ok := h.runners[addr]; ok {
		return conn, true, nil
	}

	newConn, err := server.LoggingGrpcClient(addr, h.runnerMonitoringOpts)
	if err != nil {
		return nil, false, fmt.Errorf("failed to initialize grpc client connection: %w", err)
	}

	h.runners[addr] = newConn
	return newConn, false, nil
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
