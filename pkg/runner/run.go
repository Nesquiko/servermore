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

	"github.com/Nesquiko/servermore/pkg/server"
	grpc "google.golang.org/grpc"
)

type RunnerConfig struct {
	AppName    string
	CommitHash string
	Env        string

	Addr                  string
	CommanderAddress      string
	InstanceShutdownAfter time.Duration
	FuncStorageRoot       string
}

func Run(ctx context.Context, conf RunnerConfig) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	monitoringOpts := server.MonitoringOpts{
		IsDev:      conf.Env != "PROD",
		AppName:    conf.AppName,
		AppVersion: conf.CommitHash,
		Env:        conf.Env,
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

	grpcServer := server.InstrumentedGrpcServer(monitoringOpts)
	RegisterRunnerServer(grpcServer, runnerServer)

	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			errCh <- fmt.Errorf("grpc server failed to serve: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down runner")

		runnerCloser()
		grpcServer.GracefulStop()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		otelShutdown(shutdownCtx)
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
