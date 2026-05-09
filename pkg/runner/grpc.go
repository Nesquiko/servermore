package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	commanderclient "github.com/Nesquiko/servermore/pkg/api/commander-client"
	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/commander"
	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	"github.com/Nesquiko/servermore/pkg/guest"
	runnergrpc "github.com/Nesquiko/servermore/pkg/runner/grpc"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/google/uuid"
)

type runnerGrpcServer struct {
	runnergrpc.UnimplementedRunnerServer

	runnerId            int64
	commanderGrpcClient commandergrpc.CommanderClient
	commanderHttpClient *commanderclient.Client

	instances *InstancesStates
	downloads *DownloadsSyncMap

	downloadsStorageRoot string

	instanceShutdownAfter time.Duration
	instanceGracePeriod   time.Duration

	monitoringOpts   server.MonitoringOpts
	metricsCollector *MetricsCollector
}

var _ runnergrpc.RunnerServer = (*runnerGrpcServer)(nil)

func newRunnerGrpcServer(
	ctx context.Context,
	conf RunnerConfig,
	monitoringOpts server.MonitoringOpts,
) (*runnerGrpcServer, func(), error) {
	err := server.CreateDirIfNotExists(conf.FuncStorageRoot)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"creating storage root %q failed: %w",
			conf.FuncStorageRoot,
			err,
		)
	}

	commanderHttpAddr := fmt.Sprintf("http://%s:%s", conf.CommanderHost, conf.CommanderHttpPort)
	httpClient, err := commanderclient.NewClient(commanderHttpAddr)
	if err != nil {
		slog.Error(
			"failed to initialize http commander client",
			"commander.http.address", commanderHttpAddr,
			"error", err,
		)
		return nil, nil, fmt.Errorf("creating commander http client failed: %w", err)
	}

	commanderGrpcAddr := fmt.Sprintf("%s:%s", conf.CommanderHost, conf.CommanderGrpcPort)
	conn, err := server.LoggingGrpcClient(commanderGrpcAddr, monitoringOpts)
	if err != nil {
		slog.Error(
			"failed to initialize grpc commander client",
			"commander.grpc.address", commanderGrpcAddr,
			"error", err,
		)
		return nil, nil, fmt.Errorf("failed to initialize grpc commander client: %w", err)
	}
	closer := func() {
		server.Close(conn)
	}

	client := commandergrpc.NewCommanderClient(conn)

	resp, err := client.RegisterRunner(ctx, &commandergrpc.RegisterRunnerRequest{Addr: conf.Addr})
	if err != nil {
		closer()
		return nil, nil, fmt.Errorf("registration with commander failed: %w", err)
	}
	slog.Info("registration with commander successful", "runner.id", resp.RunnerId)

	return &runnerGrpcServer{
		runnerId:              resp.RunnerId,
		commanderGrpcClient:   client,
		commanderHttpClient:   httpClient,
		instances:             NewInstanceStates(),
		downloads:             NewDownloadsSyncMap(),
		instanceShutdownAfter: conf.InstanceShutdownAfter,
		instanceGracePeriod:   conf.InstanceGracePeriod,
		monitoringOpts:        monitoringOpts,
		downloadsStorageRoot:  conf.FuncStorageRoot,
		metricsCollector:      NewMetricsCollector(),
	}, closer, nil
}

// Heartbeat implements [RunnerServer].
func (r *runnerGrpcServer) Heartbeat(
	context.Context,
	*runnergrpc.HeartbeatRequest,
) (*runnergrpc.HeartbeatResponse, error) {
	depths := r.instances.QueueDepths(r.instanceGracePeriod)
	metrics, err := r.metricsCollector.Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to collect metrics: %w", err)
	}
	return &runnergrpc.HeartbeatResponse{
		QueueDepths:       depths,
		CpuPercent:        metrics.CPUPercent,
		UnusedMemoryBytes: metrics.UnusedMemoryBytes,
	}, nil
}

// PrepareFunctionInstance implements [RunnerServer].
func (r *runnerGrpcServer) PrepareFunctionInstance(
	ctx context.Context,
	req *runnergrpc.PrepareInstanceRequest,
) (*runnergrpc.PrepareInstanceResponse, error) {
	funcPath := r.pathOnRunner(req.FunctionPath)
	meta := &server.DownloadMeta{FunctionID: req.FunctionId, DownloadPath: req.FunctionPath}
	server.SetDownloadMeta(ctx, meta)

	exists, err := funcFileExists(funcPath)
	if err != nil {
		return nil, fmt.Errorf("function path existence check fail: %w", err)
	}

	if exists {
		meta.ReusedFromFS = true
		meta.StoredPath = funcPath
		instanceId, err := r.startInstance(ctx, funcPath)
		if err != nil {
			return nil, fmt.Errorf("starting instance failed: %w", err)
		}
		return &runnergrpc.PrepareInstanceResponse{
			InstanceId: instanceId.String(),
			Downloaded: false,
		}, nil
	}

	isDownloading, resultConsumers, downloadCh := r.downloads.IsDownloadedOrStartDownload(funcPath)

	if isDownloading {
		meta.ReusedInFlight = true
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case downloadResult := <-downloadCh:
			if downloadResult.err != nil {
				return nil, fmt.Errorf("download result errored: %w", downloadResult.err)
			}
			meta.StoredPath = downloadResult.path

			instanceId, err := r.startInstance(ctx, downloadResult.path)
			if err != nil {
				return nil, fmt.Errorf("starting instance downloaded by other failed: %w", err)
			}
			return &runnergrpc.PrepareInstanceResponse{
				InstanceId: instanceId.String(),
				Downloaded: false,
			}, nil
		}
	}

	funcFilePath, bytesWritten, downloadTook, err := r.downloadFunction(ctx, req.FunctionId)
	r.downloads.Delete(funcPath)
	resultConsumers.SubmitResult(DownloadResult{path: funcFilePath, err: err})
	if err != nil {
		return nil, fmt.Errorf("download errored: %w", err)
	}
	meta.Downloaded = true
	meta.StoredPath = funcFilePath
	meta.BytesWritten = bytesWritten
	meta.DownloadTook = downloadTook

	instanceId, err := r.startInstance(ctx, funcFilePath)
	if err != nil {
		return nil, fmt.Errorf("starting downloaded instance failed: %w", err)
	}
	return &runnergrpc.PrepareInstanceResponse{
		InstanceId: instanceId.String(),
		Downloaded: true,
	}, nil
}

func (r *runnerGrpcServer) startInstance(
	ctx context.Context,
	funcFilePath server.AbsolutePath,
) (uuid.UUID, error) {
	instanceId, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to construct uuid v7: %w", err)
	}

	meta := &server.InstanceStartMeta{FunctionPath: funcFilePath, InstanceID: instanceId.String()}
	server.SetInstanceStartMeta(ctx, meta)

	isStarting, idConsumers, idCh := r.instances.IsStartingOrStartIt(funcFilePath)
	if isStarting {
		idResult := <-idCh
		if idResult.err != nil {
			return uuid.Nil, fmt.Errorf(
				"starting instance for %q errored: %w",
				funcFilePath, idResult.err,
			)
		}
		assert.That(idResult.instanceId != uuid.Nil, "instance id was nil")

		meta.InstanceID = idResult.instanceId.String()
		meta.ReusedAssigned = true
		return idResult.instanceId, nil
	}

	funcRuntime := DetermineFuncRuntime()
	meta.RuntimeType = funcRuntime.Type()

	err = funcRuntime.Start(ctx, funcFilePath)
	if err != nil {
		r.instances.RemoveStartingInstance(funcFilePath)
		idConsumers.SubmitResult(StartInstanceResult{err: err})
		return uuid.Nil, fmt.Errorf("failed to start the instance: %w", err)
	}
	instance := r.instances.Submit(funcFilePath, instanceId, r.instanceShutdownAfter, funcRuntime)
	go r.shutdownAfterTime(instance)

	idConsumers.SubmitResult(StartInstanceResult{instanceId: instanceId})

	return instanceId, nil
}

func (r *runnerGrpcServer) shutdownAfterTime(instance *instanceState) {
	<-instance.lastUsedTimer.C
	r.instances.StopInstance(instance)
}

// InvokeFunctionInstance implements [RunnerServer].
func (r *runnerGrpcServer) InvokeFunctionInstance(
	ctx context.Context,
	req *runnergrpc.InvokeInstanceRequest,
) (*runnergrpc.InvokeInstanceResponse, error) {
	startTime := time.Now()
	meta := &server.InvokeMeta{
		InstanceID:       req.InstanceId,
		Method:           req.Method,
		Path:             req.Path,
		RequestBodyBytes: len(req.Body),
		HeadersCount:     len(req.Headers),
	}
	server.SetInvokeMeta(ctx, meta)

	instanceId, err := uuid.Parse(req.InstanceId)
	if err != nil {
		meta.InvocationTook = time.Since(startTime)
		return nil, fmt.Errorf("invalid uuid instanceId: %w", err)
	}

	invocationReq := InvocationRequest{
		Method:  req.Method,
		Path:    req.Path,
		Headers: req.Headers,
		Body:    req.Body,
	}

	isRunning, instance, err := r.instances.InstanceState(instanceId)
	if err != nil {
		meta.InvocationTook = time.Since(startTime)
		return nil, fmt.Errorf("invoking instance %q failed: %w", req.InstanceId, err)
	}
	meta.WorkerAlreadyRunning = isRunning
	meta.StartedWorker = !isRunning
	meta.FunctionPath = instance.funcPath
	respCh := instance.AddToQueue(invocationReq)
	meta.QueueDepthAtEnqueue = len(instance.queue)

	if !isRunning {
		go r.instanceWorker(instance)
	}

	select {
	case <-ctx.Done():
		meta.InvocationTook = time.Since(startTime)
		return nil, ctx.Err()
	case result := <-respCh:
		meta.InvocationTook = time.Since(startTime)
		if result.err != nil {
			return result.resp, result.err
		}
		if result.resp != nil {
			meta.ResponseStatusCode = result.resp.StatusCode
			meta.ResponseBodyBytes = len(result.resp.Body)
		}
		return result.resp, result.err
	}
}

const WorkerIdleTimeout = 5 * time.Second

func (r *runnerGrpcServer) instanceWorker(instance *instanceState) {
	timer := time.NewTimer(WorkerIdleTimeout)
	defer timer.Stop()

outer:
	for {
		select {
		case <-instance.workerCtx.Done():
			break outer
		case <-timer.C:
			instance.opened.Store(false)
			break outer

		case req, ok := <-instance.queue:
			if !ok {
				instance.opened.Store(false)
				break outer
			}

			resp, err := instance.runtime.Invoke(instance.workerCtx, &guest.InvocationRequest{Method: req.req.Method, Path: req.req.Path, Headers: req.req.Headers, Body: req.req.Body})
			if err != nil {
				req.resCh <- &InvocationResult{err: err}
			} else {
				req.resCh <- &InvocationResult{resp: &runnergrpc.InvokeInstanceResponse{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}}
			}

			if !timer.Reset(WorkerIdleTimeout) {
				break outer
			}
		}
	}
}

func (r *runnerGrpcServer) downloadFunction(
	ctx context.Context,
	functionId string,
) (server.AbsolutePath, int64, time.Duration, error) {
	startTime := time.Now()
	resp, err := r.commanderHttpClient.DownloadFunctionBinary(ctx, functionId)
	if err != nil {
		return "", 0, 0, fmt.Errorf("download function id '%s' failed: %w", functionId, err)
	}
	defer server.Close(resp.Body)

	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", 0, 0, fmt.Errorf(
			"download failed, function with id '%s' wasn't found",
			functionId,
		)
	case http.StatusInternalServerError:
		return "", 0, 0, fmt.Errorf("download failed, commander errored")
	}

	responseFuncPath := resp.Header.Get(commander.DownloadHeaderFunctionPath)
	responseFuncName := resp.Header.Get(commander.DownloadHeaderFunctionFilename)

	funcPath := r.pathOnRunner(filepath.Join(responseFuncPath, responseFuncName))
	funcDir := filepath.Dir(funcPath)

	if err := os.MkdirAll(funcDir, 0o755); err != nil {
		return "", 0, 0, fmt.Errorf(
			"failed to create the dir %q path of downloaded function '%s': %w",
			funcPath, functionId, err,
		)
	}

	tmpFile, err := os.CreateTemp(funcDir, fmt.Sprintf("%s-*", responseFuncName))
	if err != nil {
		return "", 0, 0, fmt.Errorf(
			"failed to create temp download file in dir %q for function '%s': %w",
			funcDir, functionId, err,
		)
	}

	bytesWritten, err := io.Copy(tmpFile, resp.Body)
	if err != nil {
		server.DeleteFile(tmpFile.Name())
		return "", 0, 0, fmt.Errorf(
			"failed copy bytes of to temp function file %q for function '%s': %w",
			tmpFile.Name(), functionId, err,
		)
	}

	if err = tmpFile.Close(); err != nil {
		server.DeleteFile(tmpFile.Name())
		return "", 0, 0, fmt.Errorf(
			"failed to close temp function file %q for function '%s': %w",
			tmpFile.Name(), functionId, err,
		)
	}

	err = os.Chmod(tmpFile.Name(), 0o755)
	if err != nil {
		server.DeleteFile(tmpFile.Name())
		return "", 0, 0, fmt.Errorf(
			"failed to change perms on temp function file %q for function '%s': %w",
			tmpFile.Name(), functionId, err,
		)
	}

	err = os.Rename(tmpFile.Name(), funcPath)
	if err != nil {
		server.DeleteFile(tmpFile.Name())
		return "", 0, 0, fmt.Errorf(
			"failed to rename temp function file %q to its original name %q: %w",
			tmpFile.Name(), funcPath, err,
		)
	}

	return funcPath, bytesWritten, time.Since(startTime), nil
}

func funcFileExists(path string) (bool, error) {
	stats, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if stats.IsDir() {
		return false, errors.New("path is a directory")
	}

	return true, nil
}

func (r *runnerGrpcServer) pathOnRunner(path server.AbsolutePath) server.AbsolutePath {
	return filepath.Join(r.downloadsStorageRoot, path)
}
