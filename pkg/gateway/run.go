package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	otelchimetric "github.com/riandyrn/otelchi/metric"
	"google.golang.org/grpc"
)

type GatewayConfig struct {
	AppName string
	Env     server.Environment

	Address string

	CommanderAddr                 string
	CommanderClientMonitoringOpts server.MonitoringOpts
	RunnerClientMonitoringOpts    server.MonitoringOpts
}

func Run(ctx context.Context, conf GatewayConfig) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	monitoringOpts := server.MonitoringOpts{
		Env:     conf.Env,
		AppName: conf.AppName,
	}

	otelCfg, otelShutdown, err := server.InitHttpOTEL(ctx, monitoringOpts)
	if err != nil {
		slog.Error("failed to initialize OTEL", "error", err)
		return fmt.Errorf("failed to initialize OTEL: %w", err)
	}
	defer server.CloseWithCtx(ctx, otelShutdown)

	commanderClient, conn, err := commandergrpc.CreateCommanderClient(
		conf.CommanderAddr,
		conf.CommanderClientMonitoringOpts,
	)
	if err != nil {
		return fmt.Errorf("failed to connect to commander: %w", err)
	}
	defer server.Close(conn)

	httpServer := &http.Server{
		Addr:    conf.Address,
		Handler: handler(commanderClient, otelCfg, monitoringOpts, conf.RunnerClientMonitoringOpts),
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info(
			"gateway starting",
			"address", conf.Address,
			"commander.address", conf.CommanderAddr,
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("interrupt received, shutting down gateway")
	case err := <-errCh:
		if err != nil {
			slog.Error("http server failed", "error", err)
			return fmt.Errorf("http server failed: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	return errors.Join(
		httpServer.Shutdown(shutdownCtx),
		otelShutdown(shutdownCtx),
	)
}

func handler(
	commanderClient commandergrpc.CommanderClient,
	otelCfg otelchimetric.BaseConfig,
	monitoringOpts server.MonitoringOpts,
	runnerMonitoringOpts server.MonitoringOpts,
) http.Handler {
	r := chi.NewRouter()
	baseUrl := fmt.Sprintf("/{%s}", FunctionIdPathParam)

	h := &gatewayHandler{
		commanderClient:      commanderClient,
		runners:              make(map[string]*grpc.ClientConn),
		runnersMu:            sync.RWMutex{},
		runnerMonitoringOpts: runnerMonitoringOpts,
	}

	r.Use(middleware.Heartbeat(server.HeartbeatEndpoint))
	r.Use(server.HttpMiddleware(otelCfg, monitoringOpts)...)

	r.Route(baseUrl, func(r chi.Router) {
		r.HandleFunc("/*", h.processFunctionRequest)
	})

	return r
}
