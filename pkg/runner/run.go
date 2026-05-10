package runner

import (
	context "context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	grpc "google.golang.org/grpc"
)

type RunnerConfig struct {
	AppName    string             `yaml:"app_name"`
	CommitHash string             `yaml:"commit_hash"`
	Env        server.Environment `yaml:"env"`

	Addr string `yaml:"addr"`

	CommanderHost     string `yaml:"commander_host"`
	CommanderHttpPort string `yaml:"commander_http_port"`
	CommanderGrpcPort string `yaml:"commander_grpc_port"`

	InstanceShutdownAfter time.Duration `yaml:"instance_shutdown_after"`
	InstanceGracePeriod   time.Duration `yaml:"instance_grace_period"`
	FuncStorageRoot       string        `yaml:"func_storage_root"`
}

func Run(ctx context.Context, conf RunnerConfig) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	monitoringOpts := server.MonitoringOpts{
		Env:        conf.Env,
		AppName:    conf.AppName,
		AppVersion: conf.CommitHash,
	}

	_, otelShutdown, err := server.InitOTEL(ctx, monitoringOpts)
	if err != nil {
		slog.Error("failed to initialize OTEL", "error", err)
		return fmt.Errorf("OTEL initialization failed: %w", err)
	}

	runnerServer, runnerCloser, err := newRunnerGrpcServer(ctx, conf, monitoringOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize runner: %w", err)
	}

	lis, err := net.Listen("tcp", conf.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on addr %q: %w", conf.Addr, err)
	}
	// lis.Close by the grpcServer

	grpcServer := server.InstrumentedGrpcServer(monitoringOpts,
		server.WithDownloadMetaHolder,
		server.WithInstanceStartMetaHolder,
		server.WithInvokeMetaHolder,
	)
	runnergrpc.RegisterRunnerServer(grpcServer, runnerServer)

	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			errCh <- fmt.Errorf("grpc server failed to serve: %w", err)
		}
		close(errCh)
	}()

	registerCtx, registerCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := runnerServer.registerWithCommander(registerCtx, conf.Addr); err != nil {
		registerCancel()
		grpcServer.GracefulStop()
		runnerCloser()
		return fmt.Errorf("failed to register runner: %w", err)
	}
	registerCancel()

	select {
	case <-ctx.Done():
		slog.Info("shutting down runner")

		grpcServer.GracefulStop()
		runnerCloser()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		server.CloseWithCtx(shutdownCtx, otelShutdown)
		return nil
	case err := <-errCh:
		runnerCloser()
		if err != nil {
			slog.Error("grpc server failed to serve", "error", err)
			return err
		}
		return nil
	}
}
