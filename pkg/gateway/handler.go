package gateway

import (
	"fmt"
	"io"
	"log/slog"
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

	// TODO add check on the function id, if it is ""

	routeResp, err := h.commanderClient.RouteFunction(
		r.Context(),
		&commandergrpc.RouteFunctionRequest{
			FunctionId: functionId,
		},
	)
	// TODO user server.Encode things so that the error goes to ctx not this log
	if err != nil {
		slog.Error("routing failed", "functionId", functionId, "error", err)
		server.InternalServerError(w, r, err)
		return
	}

	runnerConn, err := h.runnerConn(routeResp.RunnerAddr)
	// TODO user server.Encode things so that the error goes to ctx not this log
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
	// TODO user server.Encode things so that the error goes to ctx not this log
	if err != nil {
		slog.Error("invocation failed", "instanceId", routeResp.InstanceId, "error", err)
		server.InternalServerError(w, r, err)
		return
	}

	for k, v := range invokeResp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(int(invokeResp.StatusCode))
	// TODO user server.Encode things so that the error goes to ctx not this log
	if _, err := w.Write(invokeResp.Body); err != nil {
		slog.Error("failed to write response body", "error", err)
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

func (h *gatewayHandler) closeRunnerConns() {
	h.runnersMu.Lock()
	defer h.runnersMu.Unlock()

	for addr, conn := range h.runners {
		delete(h.runners, addr)
		server.Close(conn)
	}
}

func forwardedPath(path string, functionId string) string {
	assert.That(functionId != path, "function id was empty string")

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
