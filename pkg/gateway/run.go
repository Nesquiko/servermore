package gateway

import (
	"context"
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
	AppName string             `yaml:"app_name"`
	Env     server.Environment `yaml:"env"`
	OTELOn  bool               `yaml:"otel_on"`

	Address string `yaml:"address"`

	CommanderAddr                 string                `yaml:"commander_addr"`
	CommanderClientMonitoringOpts server.MonitoringOpts `yaml:"commander_client_monitoring_opts"`
	RunnerClientMonitoringOpts    server.MonitoringOpts `yaml:"runner_client_monitoring_opts"`
}

func Run(ctx context.Context, conf GatewayConfig) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	monitoringOpts := server.MonitoringOpts{
		Env:     conf.Env,
		AppName: conf.AppName,
		OTELOn:  conf.OTELOn,
	}
	server.SetDefaultLogger(monitoringOpts)

	otelCfg, otelShutdown, err := server.InitHttpOTEL(ctx, monitoringOpts)
	if err != nil {
		slog.Error("failed to initialize OTEL", "error", err)
		return fmt.Errorf("failed to initialize OTEL: %w", err)
	}

	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.CloseWithCtx(shutdownCtx, otelShutdown)
	}()

	commanderClientOpts := conf.CommanderClientMonitoringOpts
	commanderClientOpts.OTELOn = conf.OTELOn || commanderClientOpts.OTELOn
	runnerClientOpts := conf.RunnerClientMonitoringOpts
	runnerClientOpts.OTELOn = conf.OTELOn || runnerClientOpts.OTELOn

	commanderClient, conn, err := commandergrpc.CreateCommanderClient(
		conf.CommanderAddr,
		commanderClientOpts,
	)
	if err != nil {
		return fmt.Errorf("failed to connect to commander: %w", err)
	}
	defer server.Close(conn)

	h, httpHandler, err := createHttpHandler(
		commanderClient,
		otelCfg,
		monitoringOpts,
		runnerClientOpts,
	)
	if err != nil {
		return err
	}
	defer h.Close()

	httpServer := &http.Server{
		Addr:    conf.Address,
		Handler: httpHandler,
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
	return httpServer.Shutdown(shutdownCtx)
}

func createHttpHandler(
	commanderClient commandergrpc.CommanderClient,
	otelCfg otelchimetric.BaseConfig,
	monitoringOpts server.MonitoringOpts,
	runnerMonitoringOpts server.MonitoringOpts,
) (*gatewayHandler, http.Handler, error) {
	r := chi.NewRouter()
	baseUrl := fmt.Sprintf("/{%s}", FunctionIdPathParam)

	h := &gatewayHandler{
		commanderClient:      commanderClient,
		runners:              make(map[string]*grpc.ClientConn),
		runnersMu:            sync.RWMutex{},
		runnerMonitoringOpts: runnerMonitoringOpts,
	}

	r.Use(middleware.Heartbeat(server.HeartbeatEndpoint))
	r.Use(
		server.HttpMiddleware(
			otelCfg,
			monitoringOpts,
			server.WithGatewayFunctionRequestMetaHolder,
		)...)

	r.Route(baseUrl, func(r chi.Router) {
		r.HandleFunc("/*", h.processFunctionRequest)
	})

	return h, r, nil
}
