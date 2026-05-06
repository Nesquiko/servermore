package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/Nesquiko/servermore/pkg/commander"
	runner "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const FunctionIdPathParam = "functionId"

type GatewayConfig struct {
	CommanderAddr string
}

type gatewayHandler struct {
	commanderClient commander.CommanderClient
}

func Run(ctx context.Context, opts server.MonitoringOpts, conf GatewayConfig) error {
	otelCfg, shutdown, err := server.InitHttpOTEL(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to initialize OTEL: %w", err)
	}
	defer shutdown(ctx)

	conn, err := grpc.NewClient(conf.CommanderAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to commander: %w", err)
	}
	defer conn.Close()

	h := &gatewayHandler{
		commanderClient: commander.NewCommanderClient(conn),
	}

	r := chi.NewRouter()
	r.Use(middleware.Heartbeat(server.HeartbeatEndpoint))
	r.Use(server.HttpMiddleware(otelCfg, opts)...)

	// Zachytáva /{functionId} a všetko, čo nasleduje za tým
	r.Route(fmt.Sprintf("/{%s}", FunctionIdPathParam), func(r chi.Router) {
		r.HandleFunc("/*", h.processFunctionRequest)
	})

	slog.Info("gateway starting", "port", 42069, "commander", conf.CommanderAddr)
	return http.ListenAndServe(":42069", r)
}

func (h *gatewayHandler) processFunctionRequest(w http.ResponseWriter, r *http.Request) {
	functionId := chi.URLParam(r, FunctionIdPathParam)

	// 1. Spýtame sa Commandera na trasovanie
	routeResp, err := h.commanderClient.RouteFunction(r.Context(), &commander.RouteFunctionRequest{
		FunctionId: functionId,
	})
	if err != nil {
		slog.Error("routing failed", "functionId", functionId, "error", err)
		server.InternalServerError(w, r, err)
		return
	}

	// 2. Pripojíme sa k Runneru (v reálnej appke použi pool pripojení!)
	runnerConn, err := grpc.NewClient(routeResp.RunnerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.InternalServerError(w, r, fmt.Errorf("failed to connect to runner: %w", err))
		return
	}
	defer runnerConn.Close()
	runnerClient := runner.NewRunnerClient(runnerConn)

	// 3. Prečítame telo HTTP requestu
	body, err := io.ReadAll(r.Body)
	if err != nil {
		server.InternalServerError(w, r, err)
		return
	}

	// 4. Zavoláme funkciu na Runneri cez gRPC
	invokeResp, err := runnerClient.InvokeFunctionInstance(r.Context(), &runner.InvokeInstanceRequest{
		InstanceId: routeResp.InstanceId,
		Method:     r.Method,
		Path:       r.URL.Path,
		Headers:    flattenHeaders(r.Header),
		Body:       body,
	})
	if err != nil {
		slog.Error("invocation failed", "instanceId", routeResp.InstanceId, "error", err)
		server.InternalServerError(w, r, err)
		return
	}

	// 5. Preklopíme odpoveď z gRPC späť na HTTP
	for k, v := range invokeResp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(int(invokeResp.StatusCode))
	w.Write(invokeResp.Body)
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
