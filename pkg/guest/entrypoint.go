package guest

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/Nesquiko/servermore/pkg/server"
	"google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type FunctionHandler = func(context.Context, *InvocationRequest) (*InvocationResponse, error)

const (
	GuestLogComponentKey   = "component"
	GuestLogComponentValue = "guest_sdk"

	GuestAddrEnvVar = "GUEST_ADDR"
)

func Start(f FunctionHandler) {
	sdkLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With(
		slog.String(GuestLogComponentKey, GuestLogComponentValue),
	)

	if err := start(f, sdkLogger); err != nil {
		sdkLogger.Error("guest startup failed", "error", err)
		os.Exit(1)
	}
}

func start(f FunctionHandler, sdkLogger *slog.Logger) error {
	if f == nil {
		return fmt.Errorf("function handler is nil")
	}

	addr := os.Getenv(GuestAddrEnvVar)

	monitoringOpts := server.MonitoringOpts{
		AppName:    "guest-sdk",
		AppVersion: "n/a",
		Env:        "LOCAL",
	}

	if addr == "" {
		return fmt.Errorf("no addr configured")
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on addr %q: %w", addr, err)
	}
	// lis.Close by the grpcServer

	grpcServer := server.InstrumentedGrpcServer(monitoringOpts)
	RegisterGuestServer(grpcServer, &guestServer{f: f})

	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			errCh <- fmt.Errorf("grpc server failed to serve: %w", err)
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		sdkLogger.Info("shutting down guest", "signal", sig.String())
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		if err != nil {
			sdkLogger.Error("grpc server failed to serve", "error", err)
			return err
		}
		return nil
	}
}

type guestServer struct {
	UnimplementedGuestServer

	f FunctionHandler
}

var _ GuestServer = (*guestServer)(nil)

// Heartbeat implements [GuestServer].
func (g *guestServer) Heartbeat(context.Context, *HeartbeatRequest) (*HeartbeatResponse, error) {
	return &HeartbeatResponse{}, nil
}

// InvokeFunction implements [GuestServer].
func (g *guestServer) InvokeFunction(
	ctx context.Context,
	req *InvocationRequest,
) (*InvocationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	resp, err := g.f(ctx, req)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else if resp == nil {
		return nil, status.Error(codes.Internal, "response can't be empty")
	}
	return resp, nil
}
