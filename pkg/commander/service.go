package commander

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/caching"
	queries "github.com/Nesquiko/servermore/pkg/commander/queries.gen"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	"google.golang.org/grpc"
)

type CommanderServiceConfig struct{}

type CommanderService struct {
	db          CommanderDB
	funcStorage *FileSystemFunctionStorage
	cache       caching.RoutingCache

	runnerClientOpts server.MonitoringOpts
}

var (
	ErrFunctionExists   = errors.New("function with same hash already exists")
	ErrFunctionNotFound = errors.New("function not found")
)

func NewCommanderService(
	db CommanderDB,
	funcStorage *FileSystemFunctionStorage,
	runnerClientOpts server.MonitoringOpts,
	cache caching.RoutingCache,
) *CommanderService {
	return &CommanderService{
		db:               db,
		funcStorage:      funcStorage,
		runnerClientOpts: runnerClientOpts,
		cache:            cache,
	}
}

func (svc *CommanderService) PollRunnerHeartbeats(
	ctx context.Context,
	executedAt time.Time,
) error {
	panic("not yet")
}

func (svc *CommanderService) CreateFunction(
	ctx context.Context,
	funcName string,
	funcBytesReader io.ReadCloser,
) (api.Function, error) {
	funcBytes, err := io.ReadAll(funcBytesReader)
	if err != nil {
		return api.Function{}, fmt.Errorf("reading function bytes failed: %w", err)
	}
	defer server.Close(funcBytesReader)

	hash := BytesSha256(funcBytes)

	exists, err := svc.db.FunctionExistsByHash(ctx, hash)
	if err != nil {
		return api.Function{}, fmt.Errorf("check for function with hash '%X' failed: %w", hash, err)
	}

	if exists {
		return api.Function{}, ErrFunctionExists
	}

	funcPath, err := svc.funcStorage.Save(funcName, hash, funcBytes)
	if err != nil {
		return api.Function{}, err
	}

	newFunc, err := svc.db.CreateFunction(ctx, funcPath, funcName, hash)
	if err != nil {
		return api.Function{}, fmt.Errorf("persisting new function failed: %w", err)
	}

	return api.Function{Id: newFunc.ID, Name: newFunc.Name}, nil
}

func (svc *CommanderService) FunctionByID(ctx context.Context, id int64) (queries.Function, error) {
	function, err := svc.db.FunctionByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return queries.Function{}, ErrFunctionNotFound
	}
	if err != nil {
		return queries.Function{}, fmt.Errorf("querying function by id failed: %w", err)
	}

	return function, nil
}

func (svc *CommanderService) RegisterRunner(
	ctx context.Context,
	addr string,
) (queries.Runner, error) {
	meta := server.GetRegisterRunnerMeta(ctx)
	assert.That(meta != nil, "no register runner meta set in ctx")
	meta.RunnerAddr = addr

	run, err := svc.db.RunnerByAddr(ctx, addr)

	if errors.Is(err, sql.ErrNoRows) {
		return svc.persistNewRunner(ctx, addr)
	} else if err != nil {
		return queries.Runner{}, fmt.Errorf("querying runner by addr failed: %w", err)
	}
	meta.RunnerID = run.ID
	meta.PreexistingRunner = true

	return run, nil
}

// persistNewRunner tries to call heartbeat on the provided address,
// if it succeds then persists new runner.
func (svc *CommanderService) persistNewRunner(
	ctx context.Context,
	addr string,
) (queries.Runner, error) {
	meta := server.GetRegisterRunnerMeta(ctx)
	if meta != nil {
		meta.RunnerAddr = addr
	}

	runnerClient, conn, err := newRunnerClient(addr, svc.runnerClientOpts)
	if err != nil {
		return queries.Runner{}, fmt.Errorf("initializing runner at %q failed: %w", addr, err)
	}
	defer server.Close(conn)

	_, err = runnerClient.Heartbeat(ctx, &runnergrpc.HeartbeatRequest{})
	if err != nil {
		return queries.Runner{}, fmt.Errorf("runner at %q heartbeat failed: %w", addr, err)
	}
	if meta != nil {
		meta.RunnerHeartbeatOK = true
	}

	runn, err := svc.db.CreateRunner(ctx, addr)
	if err != nil {
		return queries.Runner{}, fmt.Errorf("failed to save runner at %q: %w", addr, err)
	}
	if meta != nil {
		meta.RunnerID = runn.ID
	}

	return runn, nil
}

func newRunnerClient(
	addr string,
	monitoringOpts server.MonitoringOpts,
) (runnergrpc.RunnerClient, *grpc.ClientConn, error) {
	opts := server.MonitoringOpts{
		Env:             monitoringOpts.Env,
		AppName:         fmt.Sprintf("%s-runner-client-%s", monitoringOpts.AppName, addr),
		AppVersion:      monitoringOpts.AppName,
		AdditionalAttrs: monitoringOpts.AdditionalAttrs,
		Level:           monitoringOpts.Level,
		OTELOn:          monitoringOpts.OTELOn,
	}
	conn, err := server.LoggingGrpcClient(addr, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create runner client for address %q: %w", addr, err)
	}

	return runnergrpc.NewRunnerClient(conn), conn, nil
}
