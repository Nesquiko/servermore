package testutils

import (
	"context"
	"fmt"
	"maps"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nesquiko/servermore/pkg/assert"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	grpc "google.golang.org/grpc"
)

type StubRunner struct {
	runnergrpc.UnimplementedRunnerServer

	host string
	port string

	grpcServer *grpc.Server

	mu             sync.RWMutex
	heartbeatResp  *runnergrpc.HeartbeatResponse
	heartbeatError error
	heartbeatDelay time.Duration
	heartbeatCalls atomic.Int64
}

func RunStubRunner(ctx context.Context) *StubRunner {
	port, err := RandomFreePort()
	assert.NoError(err)
	return runStubRunnerOnPort(ctx, port)
}

func RunCorrectStubRunner(ctx context.Context, resp *runnergrpc.HeartbeatResponse) *StubRunner {
	runner := RunStubRunner(ctx)
	runner.SetHeartbeat(resp, nil)
	return runner
}

func RunEmptyStubRunner(ctx context.Context) *StubRunner {
	return RunCorrectStubRunner(ctx, &runnergrpc.HeartbeatResponse{})
}

func RunErrorStubRunner(ctx context.Context, heartbeatErr error) *StubRunner {
	runner := RunStubRunner(ctx)
	runner.SetHeartbeat(nil, heartbeatErr)
	return runner
}

func RunSlowStubRunner(
	ctx context.Context,
	delay time.Duration,
	resp *runnergrpc.HeartbeatResponse,
) *StubRunner {
	runner := RunStubRunner(ctx)
	runner.SetHeartbeat(resp, nil)
	runner.SetHeartbeatDelay(delay)
	return runner
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
		mu:         sync.RWMutex{},
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

func (s *StubRunner) HeartbeatCalls() int64 {
	return s.heartbeatCalls.Load()
}

// SetHeartbeat configures the response returned by Heartbeat.
func (s *StubRunner) SetHeartbeat(resp *runnergrpc.HeartbeatResponse, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.heartbeatResp = cloneHeartbeatResponse(resp)
	s.heartbeatError = err
}

func (s *StubRunner) SetHeartbeatDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.heartbeatDelay = delay
}

func cloneHeartbeatResponse(resp *runnergrpc.HeartbeatResponse) *runnergrpc.HeartbeatResponse {
	if resp == nil {
		return &runnergrpc.HeartbeatResponse{}
	}

	cloned := &runnergrpc.HeartbeatResponse{
		CpuPercent:        resp.GetCpuPercent(),
		UnusedMemoryBytes: resp.GetUnusedMemoryBytes(),
	}

	if queueDepths := resp.GetQueueDepths(); len(queueDepths) > 0 {
		cloned.QueueDepths = make(map[string]uint32, len(queueDepths))
		maps.Copy(cloned.QueueDepths, queueDepths)
	}

	return cloned
}

func (s *StubRunner) Heartbeat(
	ctx context.Context,
	_ *runnergrpc.HeartbeatRequest,
) (*runnergrpc.HeartbeatResponse, error) {
	s.mu.RLock()
	delay := s.heartbeatDelay
	resp := cloneHeartbeatResponse(s.heartbeatResp)
	heartbeatErr := s.heartbeatError
	s.mu.RUnlock()

	s.heartbeatCalls.Add(1)

	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	if heartbeatErr != nil {
		return nil, heartbeatErr
	}

	return resp, nil
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
