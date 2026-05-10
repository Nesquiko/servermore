package commander

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/caching"
	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	"github.com/Nesquiko/servermore/pkg/routing"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	otelchimetric "github.com/riandyrn/otelchi/metric"
	grpc "google.golang.org/grpc"
)

type CommanderConfig struct {
	AppName string             `yaml:"app_name"`
	Env     server.Environment `yaml:"env"`
	OTELOn  bool               `yaml:"otel_on"`

	Host     string `yaml:"host"`
	HttpPort string `yaml:"http_port"`
	GrpcPort string `yaml:"grpc_port"`

	DbURI           string       `yaml:"db_uri"`
	FuncStorageRoot AbsolutePath `yaml:"func_storage_root"`

	RunnerHeartbeatPoll time.Duration `yaml:"runner_heartbeat_poll"`

	RunnerOverloadedQueueSize   int `yaml:"runner_overloaded_queue_size"`
	InstanceOverloadedQueueSize int `yaml:"instance_overloaded_queue_size"`
}

func (c CommanderConfig) GrpcAddr() string {
	return net.JoinHostPort(c.Host, c.GrpcPort)
}

func Run(ctx context.Context, cache caching.RoutingCache, conf CommanderConfig) error {
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
		return fmt.Errorf("OTEL initialization failed: %w", err)
	}

	funcStorage, err := NewFSFunctionStorage(conf.FuncStorageRoot)
	if err != nil {
		return fmt.Errorf("failed to initialize function storage: %w", err)
	}

	db, err := NewSQLiteDB(conf.DbURI)
	if err != nil {
		return fmt.Errorf("failed to initialize db: %w", err)
	}

	router := routing.NewNaiveRouter(
		conf.InstanceOverloadedQueueSize,
		conf.RunnerOverloadedQueueSize,
	)
	svc := NewCommanderService(
		db,
		funcStorage,
		cache,
		router,
		CommanderServiceConfig{RunnerClientOpts: server.MonitoringOpts{Env: conf.Env, AppName: conf.AppName, OTELOn: conf.OTELOn}},
	)
	runnerHeartbeatPolling(ctx, conf, svc)

	httpCloser, httpErrCh, err := runHttp(conf, svc, otelCfg, monitoringOpts)
	if err != nil {
		slog.Error("failed to start http server", "error", err)
		return fmt.Errorf("http server failed: %w", err)
	}

	grpcCloser, grpcErrCh, err := runGrpc(svc, conf, monitoringOpts)
	if err != nil {
		slog.Error("failed to start grpc server", "error", err)
		return fmt.Errorf("http server failed: %w", err)
	}

	select {
	case <-ctx.Done():
		slog.Info("interrupt received, shutting down server")
	case err := <-httpErrCh:
		if err != nil {
			slog.Error("http server failed", "error", err)
			return fmt.Errorf("http server failed: %w", err)
		}
	case err := <-grpcErrCh:
		if err != nil {
			slog.Error("grpc server failed", "error", err)
			return fmt.Errorf("grpc server failed: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	return errors.Join(
		db.Close(),
		httpCloser(shutdownCtx),
		grpcCloser(shutdownCtx),
		otelShutdown(shutdownCtx),
	)
}

func runHttp(
	conf CommanderConfig,
	service *CommanderService,
	otelCfg otelchimetric.BaseConfig,
	monitoringOpts server.MonitoringOpts,
) (func(context.Context) error, chan error, error) {
	srv, err := NewCommanderServer(service)
	if err != nil {
		slog.Error("failed to initialize server", "error", err)
		return nil, nil, fmt.Errorf("server initialization failed: %w", err)
	}

	r := chi.NewMux()
	r.Use(middleware.Heartbeat(server.HeartbeatEndpoint))

	handler := api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseRouter:       r,
		Middlewares:      createMiddleware(otelCfg, monitoringOpts),
		ErrorHandlerFunc: server.ErrorHandlerFunc,
	})

	httpServer := &http.Server{
		Addr:    net.JoinHostPort(conf.Host, conf.HttpPort),
		Handler: handler,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("commander starting http server", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	closer := func(ctx context.Context) error {
		return httpServer.Shutdown(ctx)
	}

	return closer, errCh, nil
}

func runGrpc(
	service *CommanderService,
	conf CommanderConfig,
	monitoringOpts server.MonitoringOpts,
) (func(context.Context) error, chan error, error) {
	commanderGrpc, grpcCloser, err := newCommanderGrpcServer(service)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize runner: %w", err)
	}

	lis, err := net.Listen("tcp", conf.GrpcAddr())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on addr %q: %w", conf.GrpcAddr(), err)
	}
	// lis.Close by the grpcServer

	grpcServer := server.InstrumentedGrpcServer(
		monitoringOpts,
		server.WithRegisterRunnerMetaHolder,
		server.WithRouteFunctionMetaHolder,
	)
	commandergrpc.RegisterCommanderServer(grpcServer, commanderGrpc)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("commander starting grpc server", "addr", conf.GrpcAddr())
		if err := grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			errCh <- fmt.Errorf("grpc server failed to serve: %w", err)
		}
		close(errCh)
	}()

	closer := func(context.Context) error {
		grpcServer.GracefulStop()
		grpcCloser()
		return nil
	}

	return closer, errCh, nil
}

func createMiddleware(
	otelCfg otelchimetric.BaseConfig,
	loggingOpts server.MonitoringOpts,
) []api.MiddlewareFunc {
	ms := server.HttpMiddleware(
		otelCfg,
		loggingOpts,
		server.WithCreateFunctionMetaHolder,
		server.WithDownloadFunctionBinaryMetaHolder,
	)
	out := make([]api.MiddlewareFunc, len(ms))
	for i, m := range ms {
		out[i] = api.MiddlewareFunc(m)
	}
	return out
}

func runnerHeartbeatPolling(ctx context.Context, conf CommanderConfig, svc *CommanderService) {
	assert.That(
		conf.RunnerHeartbeatPoll > 0,
		"RunnerHeartbeatPoll can't be 0 or less, it was %d",
		conf.RunnerHeartbeatPoll,
	)

	ticker := time.NewTicker(conf.RunnerHeartbeatPoll)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				if err := svc.PollRunnerHeartbeats(ctx, t); err != nil {
					slog.Error("error when polling runner heartbeats", "error", err, "time", t)
				}
			}
		}
	}()
}
