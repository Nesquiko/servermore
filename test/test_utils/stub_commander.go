package testutils

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/server"
	grpc "google.golang.org/grpc"
)

type StubCommander struct {
	commander.UnimplementedCommanderServer

	storageRoot string
	addr        string

	runnerID atomic.Int64
	server   *grpc.Server
}

func RunStubCommander(ctx context.Context) *StubCommander {
	port, err := RandomFreePort()
	assert.NoError(err)

	tmpDir, err := SubdirInTempDir("stub-commander")
	assert.NoError(err)

	addr := fmt.Sprintf("127.0.0.1:%s", port)
	stub := &StubCommander{
		storageRoot: tmpDir,
		addr:        addr,
		server:      grpc.NewServer(),
	}
	stub.runnerID.Store(0)

	listener, err := net.Listen("tcp", addr)
	assert.NoError(err)
	commander.RegisterCommanderServer(stub.server, stub)

	go func() {
		err := stub.server.Serve(listener)
		assert.NoError(err)
	}()

	err = WaitForGrpcReady(ctx, "stub-commander", addr)
	assert.NoError(err)

	return stub
}

func (s *StubCommander) Addr() string {
	return s.addr
}

func (s *StubCommander) Close() {
	assert.NoError(os.RemoveAll(s.storageRoot))
	s.server.GracefulStop()
}

func (s *StubCommander) SymlinkFile(srcPath server.AbsolutePath, filename string) {
	dstPath := filepath.Join(s.storageRoot, filename)
	if err := os.Symlink(srcPath, dstPath); err != nil {
		panic(fmt.Errorf("create stub commander function symlink: %w", err))
	}
}

func (s *StubCommander) Heartbeat(
	context.Context,
	*commander.HeartbeatRequest,
) (*commander.HeartbeatResponse, error) {
	return &commander.HeartbeatResponse{}, nil
}

func (s *StubCommander) RegisterRunner(
	context.Context,
	*commander.RegisterRunnerRequest,
) (*commander.RegisterRunnerResponse, error) {
	id := s.runnerID.Add(1)
	return &commander.RegisterRunnerResponse{RunnerId: id}, nil
}

func (s *StubCommander) DownloadFunction(
	ctx context.Context,
	req *commander.DownloadFunctionRequest,
) (*commander.DownloadFunctionResponse, error) {
	filename := fmt.Sprintf("%d.bin", req.FunctionId)
	path := filepath.Join(s.storageRoot, filename)
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read stub commander function file: %w", err)
	}

	return &commander.DownloadFunctionResponse{
		FunctionPath:     s.storageRoot,
		FunctionFilename: filename,
		FileBytes:        bytes,
	}, nil
}

func (s *StubCommander) RouteFunction(
	context.Context,
	*commander.RouteFunctionRequest,
) (*commander.RouteFunctionResponse, error) {
	panic("RouteFunction should not be called in stub commander")
}
