package testutils

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/riandyrn/otelchi/metric"
	grpc "google.golang.org/grpc"
)

type StubCommander struct {
	commander.UnimplementedCommanderServer

	storageRoot string
	host        string
	grpcPort    string
	httpPort    string

	runnerID   atomic.Int64
	grpcServer *grpc.Server

	errorOnPaths   map[server.AbsolutePath]error
	errorOnPathsMu sync.RWMutex
}

func RunStubCommander(ctx context.Context) *StubCommander {
	monitoringOpts := server.MonitoringOpts{
		AppName: "stub-commander",
	}

	stub := runGrpcStub(ctx)
	runHttpStub(ctx, stub, monitoringOpts)
	return stub
}

func runGrpcStub(ctx context.Context) *StubCommander {
	port, err := RandomFreePort()
	assert.NoError(err)

	tmpDir, err := SubdirInTempDir("stub-commander")
	assert.NoError(err)

	stub := &StubCommander{
		storageRoot:    tmpDir,
		host:           "127.0.0.1",
		grpcPort:       port,
		grpcServer:     grpc.NewServer(),
		errorOnPaths:   map[server.AbsolutePath]error{},
		errorOnPathsMu: sync.RWMutex{},
	}
	stub.runnerID.Store(0)

	listener, err := net.Listen("tcp", stub.GrpcAddr())
	assert.NoError(err)
	commander.RegisterCommanderServer(stub.grpcServer, stub)

	go func() {
		err := stub.grpcServer.Serve(listener)
		assert.NoError(err)
	}()

	err = WaitForGrpcReady(ctx, "stub-commander", stub.GrpcAddr())
	assert.NoError(err)

	return stub
}

func runHttpStub(ctx context.Context, stub *StubCommander, opts server.MonitoringOpts) {
	port, err := RandomFreePort()
	assert.NoError(err)

	stub.httpPort = port

	r := chi.NewMux()
	r.Use(middleware.Heartbeat(server.HeartbeatEndpoint))
	r.Use(server.HttpMiddleware(metric.BaseConfig{}, opts)...)

	h := api.HandlerFromMux(stub, r)
	h = server.WithAPIErrorHolder(h)
	s := &http.Server{Handler: h, Addr: stub.HttpAddr()}

	errCh := make(chan error, 1)
	go func() {
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	readyErrCh := make(chan error, 1)
	go func() {
		readyErrCh <- WaitForHttpReady(ctx, "stub-commander", "http://"+stub.HttpAddr()+server.HeartbeatEndpoint)
		close(readyErrCh)
	}()

	select {
	case err := <-errCh:
		assert.NoError(err)
	case err := <-readyErrCh:
		assert.NoError(err)
	}
}

func (s *StubCommander) Host() string {
	return s.host
}

func (s *StubCommander) GrpcPort() string {
	return s.grpcPort
}

func (s *StubCommander) HttpPort() string {
	return s.httpPort
}

func (s *StubCommander) GrpcAddr() string {
	return fmt.Sprintf("%s:%s", s.host, s.grpcPort)
}

func (s *StubCommander) HttpAddr() string {
	return fmt.Sprintf("%s:%s", s.host, s.httpPort)
}

func (s *StubCommander) Close() {
	assert.NoError(os.RemoveAll(s.storageRoot))
	s.grpcServer.GracefulStop()
}

func (s *StubCommander) MarkPathToError(funcPath server.AbsolutePath, err error) {
	s.errorOnPathsMu.Lock()
	s.errorOnPaths[funcPath] = err
	s.errorOnPathsMu.Unlock()
}

func (s *StubCommander) SymlinkFile(srcPath server.AbsolutePath, filename string) {
	dstPath := filepath.Join(s.storageRoot, filename)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		panic(fmt.Errorf("create stub commander symlink parent dir: %w", err))
	}
	if err := os.Symlink(srcPath, dstPath); err != nil {
		panic(fmt.Errorf("create stub commander function symlink: %w", err))
	}
}

func (s *StubCommander) PathFor(filename string) server.AbsolutePath {
	return filepath.Join(s.storageRoot, filename)
}

func (s *StubCommander) DeleteFile(filename string) {
	path := s.PathFor(filename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		panic(fmt.Errorf("delete stub commander file: %w", err))
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

func (s *StubCommander) RouteFunction(
	context.Context,
	*commander.RouteFunctionRequest,
) (*commander.RouteFunctionResponse, error) {
	panic("RouteFunction should not be called in stub commander")
}

// CreateFunction implements [api.ServerInterface].
func (s *StubCommander) CreateFunction(w http.ResponseWriter, r *http.Request) {
	panic("CreateFunction should not be called in stub commander")
}

// DownloadFunctionBinary implements [api.ServerInterface].
func (s *StubCommander) DownloadFunctionBinary(
	w http.ResponseWriter,
	r *http.Request,
	funcId string,
) {
	filename := funcId

	s.errorOnPathsMu.RLock()
	if err, ok := s.errorOnPaths[filename]; ok {
		server.InternalServerError(w, r, err)
		s.errorOnPathsMu.RUnlock()
		return
	}
	s.errorOnPathsMu.RUnlock()

	path := filepath.Join(s.storageRoot, filename)
	file, err := os.Open(path)
	if err != nil {
		server.InternalServerError(w, r, fmt.Errorf("open function binary: %w", err))
		return
	}

	defer server.Close(file)
	w.Header().Set(commander.DownloadHeaderFunctionID, funcId)
	w.Header().Set(commander.DownloadHeaderFunctionFilename, filepath.Base(path))
	w.Header().Set(commander.DownloadHeaderFunctionPath, filepath.Dir(path))
	w.Header().Set("Content-Type", "application/octet-stream")

	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, file); err != nil {
		return
	}
}
