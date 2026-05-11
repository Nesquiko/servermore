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
	"github.com/Nesquiko/servermore/pkg/server"
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

	prepareConfigured bool
	prepareResp       *runnergrpc.PrepareInstanceResponse
	prepareError      error
	prepareCallsCount atomic.Int64

	invokeConfigured bool
	invokeResp       *runnergrpc.InvokeInstanceResponse
	invokeError      error
	lastInvoke       *runnergrpc.InvokeInstanceRequest
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
		host: "127.0.0.1",
		port: port,
		grpcServer: grpc.NewServer(
			grpc.MaxRecvMsgSize(server.GrpcMaxBytes),
			grpc.MaxSendMsgSize(server.GrpcMaxBytes),
		),
		mu: sync.RWMutex{},
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

func (s *StubRunner) PrepareCallsCount() int64 {
	return s.prepareCallsCount.Load()
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

// SetPrepare configures the response returned by PrepareFunctionInstance.
func (s *StubRunner) SetPrepare(resp *runnergrpc.PrepareInstanceResponse, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prepareResp = clonePrepareInstanceResponse(resp)
	s.prepareError = err
	s.prepareConfigured = true
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

func clonePrepareInstanceResponse(
	resp *runnergrpc.PrepareInstanceResponse,
) *runnergrpc.PrepareInstanceResponse {
	if resp == nil {
		return &runnergrpc.PrepareInstanceResponse{}
	}

	return &runnergrpc.PrepareInstanceResponse{
		InstanceId: resp.GetInstanceId(),
		Downloaded: resp.GetDownloaded(),
	}
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
	s.prepareCallsCount.Add(1)

	s.mu.RLock()
	configured := s.prepareConfigured
	resp := clonePrepareInstanceResponse(s.prepareResp)
	prepareErr := s.prepareError
	s.mu.RUnlock()

	if !configured {
		panic("PrepareFunctionInstance called in stub runner without configuring it")
	}
	if prepareErr != nil {
		return nil, prepareErr
	}

	return resp, nil
}

func (s *StubRunner) SetInvoke(resp *runnergrpc.InvokeInstanceResponse, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.invokeResp = resp
	s.invokeError = err
	s.invokeConfigured = true
}

func (s *StubRunner) LastInvoke() *runnergrpc.InvokeInstanceRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastInvoke
}

func (s *StubRunner) InvokeFunctionInstance(
	ctx context.Context,
	req *runnergrpc.InvokeInstanceRequest,
) (*runnergrpc.InvokeInstanceResponse, error) {
	s.mu.Lock()
	s.lastInvoke = req
	configured := s.invokeConfigured
	resp := s.invokeResp
	invokeErr := s.invokeError
	s.mu.Unlock()

	if !configured {
		panic("InvokeFunctionInstance called in stub runner without configuring it")
	}

	if invokeErr != nil {
		return nil, invokeErr
	}

	return resp, nil
}
