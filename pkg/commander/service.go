package commander

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/caching"
	queries "github.com/Nesquiko/servermore/pkg/commander/queries.gen"
	"github.com/Nesquiko/servermore/pkg/routing"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	"golang.org/x/sync/errgroup"
)

type CommanderServiceConfig struct {
	RunnerClientOpts server.MonitoringOpts
}

type CommanderService struct {
	db          CommanderDB
	funcStorage *FileSystemFunctionStorage
	cache       caching.RoutingCache
	router      routing.Router

	config CommanderServiceConfig
}

var (
	ErrFunctionExists   = errors.New("function with same hash already exists")
	ErrFunctionNotFound = errors.New("function not found")
)

func NewCommanderService(
	db CommanderDB,
	funcStorage *FileSystemFunctionStorage,
	cache caching.RoutingCache,
	router routing.Router,
	config CommanderServiceConfig,
) *CommanderService {
	return &CommanderService{
		db:          db,
		funcStorage: funcStorage,
		config:      config,
		router:      router,
		cache:       cache,
	}
}

func (svc *CommanderService) PollRunnerHeartbeats(
	ctx context.Context,
	executedAt time.Time,
) error {
	startedAt := time.Now()
	runners, err := svc.db.GetAllRunners(ctx)
	if err != nil {
		return fmt.Errorf("querying runners failed: %w", err)
	}

	var eg errgroup.Group
	for _, runn := range runners {
		eg.Go(func() error {
			svc.pollRunnerHeartbeat(ctx, executedAt, runn)
			return nil
		})
	}

	err = eg.Wait()
	slog.InfoContext(ctx, "runner heartbeat polling completed",
		slog.Int("runners_checked", len(runners)),
		slog.Time("executed_at", executedAt),
		slog.Duration("took", time.Since(startedAt)),
	)
	return err
}

const runnerHeartbeatTimeout = 1 * time.Second

func (svc *CommanderService) pollRunnerHeartbeat(
	ctx context.Context,
	executedAt time.Time,
	runn queries.Runner,
) {
	runnerCtx, cancel := context.WithTimeout(ctx, runnerHeartbeatTimeout)
	defer cancel()

	runnerClient, conn, err := runnergrpc.CreateRunnerClient(runn.Addr, svc.config.RunnerClientOpts)
	if err != nil {
		slog.Error(
			"failed to create runner client for heartbeat",
			"runner.addr", runn.Addr,
			"time", executedAt,
			"error", err,
		)
		svc.evictRunner(ctx, executedAt, runn.Addr, "failed to create runner client for heartbeat")
		return
	}
	defer server.Close(conn)

	resp, err := runnerClient.Heartbeat(runnerCtx, &runnergrpc.HeartbeatRequest{})
	if err != nil {
		slog.Error(
			"runner heartbeat failed",
			"runner.addr", runn.Addr,
			"time", executedAt,
			"error", err,
		)
		svc.evictRunner(ctx, executedAt, runn.Addr, "runner heartbeat failed")
		return
	}

	if resp == nil {
		slog.Error(
			"runner heartbeat returned nil response",
			"runner.addr", runn.Addr,
			"time", executedAt,
		)
		svc.evictRunner(ctx, executedAt, runn.Addr, "runner heartbeat returned nil response")
		return
	}

	cacheCtx, cacheCancel := context.WithTimeout(ctx, runnerHeartbeatTimeout)
	defer cacheCancel()

	metrics := caching.ResourceMetrics{
		CpuPercent:        resp.GetCpuPercent(),
		UnusedMemoryBytes: resp.GetUnusedMemoryBytes(),
	}

	err = svc.cache.UpsertRunnerHeartbeat(cacheCtx, runn.Addr, resp.GetQueueDepths(), metrics)
	if err != nil {
		slog.Error(
			"failed to cache runner heartbeat",
			"runner.addr", runn.Addr,
			"time", executedAt,
			"error", err,
		)
	}
}

func (svc *CommanderService) evictRunner(
	ctx context.Context,
	executedAt time.Time,
	runnerAddr, reason string,
) {
	cacheCtx, cancel := context.WithTimeout(ctx, runnerHeartbeatTimeout)
	defer cancel()

	if err := svc.cache.RemoveRunner(cacheCtx, runnerAddr); err != nil {
		slog.Error(
			"failed to evict runner from cache",
			"runner.addr", runnerAddr,
			"time", executedAt,
			"reason", reason,
			"error", err,
		)
	}
}

func (svc *CommanderService) CreateFunction(
	ctx context.Context,
	funcName string,
	funcBytesReader io.ReadCloser,
) (api.Function, error) {
	meta := server.GetCreateFunctionMeta(ctx)
	assert.That(meta != nil, "meta was nil")
	meta.FunctionName = funcName

	var (
		funcBytes []byte
		hash      []byte
		funcPath  string
	)

	defer server.Close(funcBytesReader)

	funcBytes, err := io.ReadAll(funcBytesReader)
	if err != nil {
		return api.Function{}, fmt.Errorf("reading function bytes failed: %w", err)
	}

	hash = BytesSha256(funcBytes)
	meta.FunctionBytes = len(funcBytes)
	meta.FunctionHash = fmt.Sprintf("%X", hash)

	exists, err := svc.db.FunctionExistsByHash(ctx, hash)
	if err != nil {
		return api.Function{}, fmt.Errorf("check for function with hash '%X' failed: %w", hash, err)
	}

	if exists {
		meta.FunctionAlreadyExists = true
		return api.Function{}, ErrFunctionExists
	}

	funcPath, err = svc.funcStorage.Save(funcName, hash, funcBytes)
	if err != nil {
		return api.Function{}, fmt.Errorf("failed to save function binary to storage: %w", err)
	}
	meta.FunctionPath = funcPath

	newFunc, err := svc.db.CreateFunction(ctx, funcPath, funcName, hash)
	if err != nil {
		return api.Function{}, fmt.Errorf("persisting new function failed: %w", err)
	}

	meta.FunctionID = newFunc.ID
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
	assert.That(meta != nil, "meta was nil")
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

func (svc *CommanderService) RouteFunction(
	ctx context.Context,
	functionId string,
) (routing.Routing, error) {
	meta := server.GetRouteFunctionMeta(ctx)
	assert.That(meta != nil, "meta was nil")

	routingData, err := svc.router.Route(ctx, functionId, svc.cache)
	if errors.Is(err, sql.ErrNoRows) {
		return routing.Routing{}, ErrFunctionNotFound
	} else if prepareErr, isErr := errors.AsType[*routing.ErrPrepareInstance](err); isErr {
		meta.PreparedInstance = true
		prepareStart := time.Now()
		routingData, err := svc.prepareInstance(ctx, prepareErr.FunctionId, prepareErr.RunnerAddr)
		meta.PrepareTook = time.Since(prepareStart)
		meta.RunnerAddr = routingData.RunnerAddr
		meta.InstanceID = routingData.InstanceId
		return routingData, err
	} else if err != nil {
		return routing.Routing{}, fmt.Errorf("router failed: %w", err)
	}
	meta.CacheHit = true
	meta.RunnerAddr = routingData.RunnerAddr
	meta.InstanceID = routingData.InstanceId
	return routingData, nil
}

func (svc *CommanderService) prepareInstance(
	ctx context.Context,
	functionId, runnerAddr string,
) (routing.Routing, error) {
	funcId, err := strconv.ParseInt(functionId, 10, 0)
	if err != nil {
		return routing.Routing{}, fmt.Errorf(
			"failed to convert functionId %q to int: %w",
			functionId, err,
		)
	}

	function, err := svc.db.FunctionByID(ctx, funcId)
	if errors.Is(err, sql.ErrNoRows) {
		return routing.Routing{}, ErrFunctionNotFound
	} else if err != nil {
		return routing.Routing{}, fmt.Errorf(
			"failed to read function by id %q: %w",
			functionId, err,
		)
	}

	runnerClient, conn, err := runnergrpc.CreateRunnerClient(
		runnerAddr,
		svc.config.RunnerClientOpts,
	)
	if err != nil {
		return routing.Routing{}, fmt.Errorf("failed to construct runner client: %w", err)
	}
	defer server.Close(conn)

	resp, err := runnerClient.PrepareFunctionInstance(ctx, &runnergrpc.PrepareInstanceRequest{
		FunctionId:   functionId,
		FunctionPath: function.Path,
	})
	if err != nil {
		return routing.Routing{}, fmt.Errorf("prepare instance call failed: %w", err)
	}
	return routing.Routing{RunnerAddr: runnerAddr, InstanceId: resp.InstanceId}, nil
}

// persistNewRunner tries to call heartbeat on the provided address,
// if it succeds then persists new runner.
func (svc *CommanderService) persistNewRunner(
	ctx context.Context,
	addr string,
) (queries.Runner, error) {
	meta := server.GetRegisterRunnerMeta(ctx)
	assert.That(meta != nil, "meta was nil")
	meta.RunnerAddr = addr

	runnerClient, conn, err := runnergrpc.CreateRunnerClient(addr, svc.config.RunnerClientOpts)
	if err != nil {
		return queries.Runner{}, fmt.Errorf("initializing runner at %q failed: %w", addr, err)
	}
	defer server.Close(conn)

	heartbeatStartedAt := time.Now()
	_, err = runnerClient.Heartbeat(ctx, &runnergrpc.HeartbeatRequest{})
	heartbeatTook := time.Since(heartbeatStartedAt)
	meta.HeartbeatTook = heartbeatTook
	if err != nil {
		return queries.Runner{}, fmt.Errorf("runner at %q heartbeat failed: %w", addr, err)
	}
	meta.RunnerHeartbeatOK = true

	persistStartedAt := time.Now()
	runn, err := svc.db.CreateRunner(ctx, addr)
	persistTook := time.Since(persistStartedAt)
	meta.PersistTook = persistTook
	if err != nil {
		return queries.Runner{}, fmt.Errorf("failed to save runner at %q: %w", addr, err)
	}
	meta.RunnerID = runn.ID

	return runn, nil
}
