package runner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/guest"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/google/uuid"
)

type runnerGrpcServer struct {
	UnimplementedRunnerServer

	runnerId        int64
	commanderClient commander.CommanderClient

	instances *InstancesStates
	downloads *DownloadsSyncMap

	downloadsStorageRoot string

	instanceShutdownAfter time.Duration

	monitoringOpts server.MonitoringOpts
}

var _ RunnerServer = (*runnerGrpcServer)(nil)

func newRunnerGrpcServer(
	ctx context.Context,
	conf RunnerConfig,
	monitoringOpts server.MonitoringOpts,
) (*runnerGrpcServer, func(), error) {
	err := server.CreateDirIfNotExists(conf.FuncStorageRoot)
	if err != nil {
		return nil, nil, err
	}

	conn, err := server.InstrumentedGrpcClient(conf.CommanderAddress, monitoringOpts)
	if err != nil {
		slog.Error(
			"failed to initialize grpc commander client",
			"commander.address", conf.CommanderAddress,
			"error", err,
		)
		return nil, nil, fmt.Errorf("failed to initialize grpc commander client: %w", err)
	}
	closer := func() {
		if err := conn.Close(); err != nil {
			slog.Error("failed to close grpc commander connection", "error", err)
		}
	}

	client := commander.NewCommanderClient(conn)

	resp, err := client.RegisterRunner(ctx, &commander.RegisterRunnerRequest{Addr: conf.Addr})
	if err != nil {
		closer()
		return nil, nil, fmt.Errorf("registration with commander failed: %w", err)
	}
	slog.Info("registration with commander successful", "runner.id", resp.RunnerId)

	return &runnerGrpcServer{
		runnerId:              resp.RunnerId,
		commanderClient:       client,
		instances:             NewInstanceStates(),
		downloads:             NewDownloadsSyncMap(),
		instanceShutdownAfter: conf.InstanceShutdownAfter,
		monitoringOpts:        monitoringOpts,
		downloadsStorageRoot:  conf.FuncStorageRoot,
	}, closer, nil
}

// Heartbeat implements [RunnerServer].
func (r *runnerGrpcServer) Heartbeat(
	context.Context,
	*HeartbeatRequest,
) (*HeartbeatResponse, error) {
	depths := r.instances.QueueDepths()
	return &HeartbeatResponse{QueueDepths: depths}, nil
}

// PrepareFunctionInstance implements [RunnerServer].
func (r *runnerGrpcServer) PrepareFunctionInstance(
	ctx context.Context,
	req *PrepareInstanceRequest,
) (*PrepareInstanceResponse, error) {
	funcPath := r.pathOnRunner(req.FunctionPath)
	exists, err := funcFileExists(funcPath)
	if err != nil {
		return nil, fmt.Errorf("function path existence check fail: %w", err)
	}

	if exists {
		instanceId, err := r.startInstance(ctx, funcPath)
		if err != nil {
			return nil, fmt.Errorf("starting instance failed: %w", err)
		}
		return &PrepareInstanceResponse{InstanceId: instanceId.String(), Downloaded: false}, nil
	}

	isDownloading, resultConsumers, downloadCh := r.downloads.IsDownloadedOrStartDownload(funcPath)

	if isDownloading {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case downloadResult := <-downloadCh:
			if downloadResult.err != nil {
				return nil, fmt.Errorf("download result errored: %w", downloadResult.err)
			}

			instanceId, err := r.startInstance(ctx, downloadResult.path)
			if err != nil {
				return nil, fmt.Errorf("starting instance downloaded by other failed: %w", err)
			}
			return &PrepareInstanceResponse{InstanceId: instanceId.String(), Downloaded: false}, nil
		}
	}

	funcFilePath, err := r.downloadFunction(ctx, req.FunctionId)
	r.downloads.Delete(funcFilePath)
	resultConsumers.SubmitResult(DownloadResult{path: funcFilePath, err: err})
	if err != nil {
		return nil, fmt.Errorf("download errorred: %w", err)
	}

	instanceId, err := r.startInstance(ctx, funcFilePath)
	if err != nil {
		return nil, fmt.Errorf("starting downloaded instance failed: %w", err)
	}
	return &PrepareInstanceResponse{InstanceId: instanceId.String(), Downloaded: true}, nil
}

func (r *runnerGrpcServer) startInstance(
	ctx context.Context,
	funcFilePath server.AbsolutePath,
) (uuid.UUID, error) {
	instanceId, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to construct uuid v7: %w", err)
	}

	isAssigned, idConsumers, idCh := r.instances.IsAssignedOrStartIt(funcFilePath)
	if isAssigned {
		return <-idCh, nil
	}

	funcRuntime := DetermineFuncRuntime()
	err = funcRuntime.Start(ctx, funcFilePath, r.monitoringOpts)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to start the instance: %w", err)
	}
	instance := r.instances.Submit(funcFilePath, instanceId, r.instanceShutdownAfter, funcRuntime)
	go r.shutdownAfterTime(instance)

	idConsumers.SubmitResult(instanceId)

	return instanceId, nil
}

func (r *runnerGrpcServer) shutdownAfterTime(instance *instanceState) {
	<-instance.lastUsedTimer.C
	r.instances.StopInstance(instance)
}

// InvokeFunctionInstance implements [RunnerServer].
func (r *runnerGrpcServer) InvokeFunctionInstance(
	ctx context.Context,
	req *InvokeInstanceRequest,
) (*InvokeInstanceResponse, error) {
	instanceId, err := uuid.Parse(req.InstanceId)
	if err != nil {
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
		return nil, fmt.Errorf("invoking instance %q failed: %w", req.InstanceId, err)
	}
	respCh := instance.AddToQueue(invocationReq)

	if !isRunning {
		go r.instanceWorker(instance)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-respCh:
		return result.resp, result.err
	}
}

const WorkerIdleTimeoutSeconds = 5 * time.Second

func (r *runnerGrpcServer) instanceWorker(instance *instanceState) {
	timer := time.NewTimer(WorkerIdleTimeoutSeconds)
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
				req.resCh <- &InvocationResult{resp: &InvokeInstanceResponse{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}}
			}

			if !timer.Reset(WorkerIdleTimeoutSeconds) {
				break outer
			}
		}
	}
}

func (r *runnerGrpcServer) downloadFunction(
	ctx context.Context,
	functionId int64,
) (server.AbsolutePath, error) {
	resp, err := r.commanderClient.DownloadFunction(
		ctx,
		&commander.DownloadFunctionRequest{FunctionId: functionId},
	)
	if err != nil {
		return "", fmt.Errorf("dowload function id '%d' failed: %w", functionId, err)
	}

	funcPath := filepath.Join(resp.FunctionPath, resp.FunctionFilename)
	funcPath = r.pathOnRunner(funcPath)

	err = server.CreateFile(funcPath, resp.FileBytes)
	if err != nil {
		return "", fmt.Errorf("creating function file at %q failed: %w", funcPath, err)
	}

	return funcPath, nil
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
