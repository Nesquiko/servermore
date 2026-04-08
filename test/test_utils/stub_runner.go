package testutils

import (
	"context"
	"fmt"
	"net"

	"github.com/Nesquiko/servermore/pkg/assert"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	grpc "google.golang.org/grpc"
)

type StubRunner struct {
	runnergrpc.UnimplementedRunnerServer

	host string
	port string

	grpcServer *grpc.Server
}

func RunStubRunner(ctx context.Context) *StubRunner {
	port, err := RandomFreePort()
	assert.NoError(err)
	return runStubRunnerOnPort(ctx, port)
}

func RunStubRunnerOnPort(ctx context.Context, port string) *StubRunner {
	return runStubRunnerOnPort(ctx, port)
}

func runStubRunnerOnPort(ctx context.Context, port string) *StubRunner {
	assert.That(port != "", "stub runner port must not be empty")

	stub := &StubRunner{
		host:       "127.0.0.1",
		port:       port,
		grpcServer: grpc.NewServer(),
	}

	listener, err := net.Listen("tcp", stub.GrpcAddr())
	assert.NoError(err)
	runnergrpc.RegisterRunnerServer(stub.grpcServer, stub)

	go func() {
		err := stub.grpcServer.Serve(listener)
		assert.NoError(err)
	}()

	err = WaitForGrpcReady(ctx, "stub-runner", stub.GrpcAddr())
	assert.NoError(err)

	return stub
}

func (s *StubRunner) Host() string {
	return s.host
}

func (s *StubRunner) Port() string {
	return s.port
}

func (s *StubRunner) GrpcAddr() string {
	return fmt.Sprintf("%s:%s", s.host, s.port)
}

func (s *StubRunner) Close() {
	s.grpcServer.GracefulStop()
}

func (s *StubRunner) Heartbeat(
	context.Context,
	*runnergrpc.HeartbeatRequest,
) (*runnergrpc.HeartbeatResponse, error) {
	return &runnergrpc.HeartbeatResponse{}, nil
}

func (s *StubRunner) PrepareFunctionInstance(
	context.Context,
	*runnergrpc.PrepareInstanceRequest,
) (*runnergrpc.PrepareInstanceResponse, error) {
	panic("PrepareFunctionInstance should not be called in stub runner")
}

func (s *StubRunner) InvokeFunctionInstance(
	context.Context,
	*runnergrpc.InvokeInstanceRequest,
) (*runnergrpc.InvokeInstanceResponse, error) {
	panic("InvokeFunctionInstance should not be called in stub runner")
}
