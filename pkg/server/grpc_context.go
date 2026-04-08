package server

import (
	"context"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	grpc "google.golang.org/grpc"
)

type (
	downloadMetaKey       struct{}
	instanceStartMetaKey  struct{}
	invokeMetaKey         struct{}
	registerRunnerMetaKey struct{}

	downloadMetaHolder struct {
		Meta *DownloadMeta
	}

	instanceStartMetaHolder struct {
		Meta *InstanceStartMeta
	}

	invokeMetaHolder struct {
		Meta *InvokeMeta
	}

	registerRunnerMetaHolder struct {
		Meta *RegisterRunnerMeta
	}
)

type DownloadMeta struct {
	FunctionID     string
	Downloaded     bool
	DownloadPath   string
	StoredPath     string
	BytesWritten   int64
	DownloadTook   time.Duration
	ReusedFromFS   bool
	ReusedInFlight bool
}

func (m DownloadMeta) Fields() logging.Fields {
	return logging.Fields{
		"download.function_id", m.FunctionID,
		"download.downloaded", m.Downloaded,
		"download.download_path", m.DownloadPath,
		"download.stored_path", m.StoredPath,
		"download.bytes_written", m.BytesWritten,
		"download.took", m.DownloadTook,
		"download.reused_from_fs", m.ReusedFromFS,
		"download.reused_inflight", m.ReusedInFlight,
	}
}

type InstanceStartMeta struct {
	FunctionPath   string
	InstanceID     string
	RuntimeType    string
	InstanceAddr   string
	StartTook      time.Duration
	ReusedAssigned bool
}

func (m InstanceStartMeta) Fields() logging.Fields {
	return logging.Fields{
		"instance_start.function_path", m.FunctionPath,
		"instance_start.instance_id", m.InstanceID,
		"instance_start.runtime_type", m.RuntimeType,
		"instance_start.instance_addr", m.InstanceAddr,
		"instance_start.start_took", m.StartTook,
		"instance_start.reused_assigned", m.ReusedAssigned,
	}
}

type InvokeMeta struct {
	InstanceID           string
	FunctionPath         string
	Method               string
	Path                 string
	RequestBodyBytes     int
	HeadersCount         int
	WorkerAlreadyRunning bool
	StartedWorker        bool
	QueueDepthAtEnqueue  int
	InvocationTook       time.Duration
	ResponseStatusCode   uint32
	ResponseBodyBytes    int
}

type RegisterRunnerMeta struct {
	RunnerAddr        string
	RunnerID          int64
	ExistingRunner    bool
	RunnerHeartbeatOK bool
	RegistrationTook  time.Duration
}

func (m RegisterRunnerMeta) Fields() logging.Fields {
	return logging.Fields{
		"register_runner.addr", m.RunnerAddr,
		"register_runner.id", m.RunnerID,
		"register_runner.existing", m.ExistingRunner,
		"register_runner.heartbeat_ok", m.RunnerHeartbeatOK,
		"register_runner.took", m.RegistrationTook,
	}
}

func (m InvokeMeta) Fields() logging.Fields {
	return logging.Fields{
		"invoke.instance_id", m.InstanceID,
		"invoke.func.path", m.FunctionPath,
		"invoke.method", m.Method,
		"invoke.path", m.Path,
		"invoke.request_body_bytes", m.RequestBodyBytes,
		"invoke.headers_count", m.HeadersCount,
		"invoke.worker_already_running", m.WorkerAlreadyRunning,
		"invoke.started_worker", m.StartedWorker,
		"invoke.queue_depth_at_enqueue", m.QueueDepthAtEnqueue,
		"invoke.took", m.InvocationTook,
		"invoke.response_status_code", m.ResponseStatusCode,
		"invoke.response_body_bytes", m.ResponseBodyBytes,
	}
}

func WithDownloadMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &downloadMetaHolder{}
	ctx = context.WithValue(ctx, downloadMetaKey{}, holder)
	return handler(ctx, req)
}

func WithInstanceStartMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &instanceStartMetaHolder{}
	ctx = context.WithValue(ctx, instanceStartMetaKey{}, holder)
	return handler(ctx, req)
}

func WithInvokeMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &invokeMetaHolder{}
	ctx = context.WithValue(ctx, invokeMetaKey{}, holder)
	return handler(ctx, req)
}

func WithRegisterRunnerMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &registerRunnerMetaHolder{}
	ctx = context.WithValue(ctx, registerRunnerMetaKey{}, holder)
	return handler(ctx, req)
}

func GetDownloadMeta(ctx context.Context) *DownloadMeta {
	holder, ok := ctx.Value(downloadMetaKey{}).(*downloadMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetDownloadMeta(ctx context.Context, meta *DownloadMeta) {
	holder, ok := ctx.Value(downloadMetaKey{}).(*downloadMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}

func GetInstanceStartMeta(ctx context.Context) *InstanceStartMeta {
	holder, ok := ctx.Value(instanceStartMetaKey{}).(*instanceStartMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetInstanceStartMeta(ctx context.Context, meta *InstanceStartMeta) {
	holder, ok := ctx.Value(instanceStartMetaKey{}).(*instanceStartMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}

func GetInvokeMeta(ctx context.Context) *InvokeMeta {
	holder, ok := ctx.Value(invokeMetaKey{}).(*invokeMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetInvokeMeta(ctx context.Context, meta *InvokeMeta) {
	holder, ok := ctx.Value(invokeMetaKey{}).(*invokeMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}

func GetRegisterRunnerMeta(ctx context.Context) *RegisterRunnerMeta {
	holder, ok := ctx.Value(registerRunnerMetaKey{}).(*registerRunnerMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetRegisterRunnerMeta(ctx context.Context, meta *RegisterRunnerMeta) {
	holder, ok := ctx.Value(registerRunnerMetaKey{}).(*registerRunnerMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}
