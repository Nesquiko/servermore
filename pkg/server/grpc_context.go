package server

import (
	"context"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	grpc "google.golang.org/grpc"
)

type (
	downloadMetaKey       struct{}
	guestInvokeMetaKey    struct{}
	instanceStartMetaKey  struct{}
	invokeMetaKey         struct{}
	routeFunctionMetaKey  struct{}
	registerRunnerMetaKey struct{}

	downloadMetaHolder struct {
		Meta *DownloadMeta
	}

	guestInvokeMetaHolder struct {
		Meta *GuestInvokeMeta
	}

	instanceStartMetaHolder struct {
		Meta *InstanceStartMeta
	}

	invokeMetaHolder struct {
		Meta *InvokeMeta
	}

	routeFunctionMetaHolder struct {
		Meta *RouteFunctionMeta
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
	SourcePath     string
	SourceFilename string
}

func (m DownloadMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m DownloadMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("download.function_id", m.FunctionID),
		attribute.Bool("download.downloaded", m.Downloaded),
		attribute.String("download.download_path", m.DownloadPath),
		attribute.String("download.stored_path", m.StoredPath),
		attribute.Int64("download.bytes_written", m.BytesWritten),
		attribute.String("download.took", m.DownloadTook.String()),
		attribute.Bool("download.reused_from_fs", m.ReusedFromFS),
		attribute.Bool("download.reused_inflight", m.ReusedInFlight),
		attribute.String("download.source_path", m.SourcePath),
		attribute.String("download.source_filename", m.SourceFilename),
	}
}

type InstanceStartMeta struct {
	FunctionPath     string
	InstanceID       string
	RuntimeType      string
	InstanceAddr     string
	StartTook        time.Duration
	ReusedAssigned   bool
	StartRetries     int
	HeartbeatRetries int
	HeartbeatTook    time.Duration
}

func (m InstanceStartMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m InstanceStartMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("instance_start.function_path", m.FunctionPath),
		attribute.String("instance_start.instance_id", m.InstanceID),
		attribute.String("instance_start.runtime_type", m.RuntimeType),
		attribute.String("instance_start.instance_addr", m.InstanceAddr),
		attribute.String("instance_start.start_took", m.StartTook.String()),
		attribute.Bool("instance_start.reused_assigned", m.ReusedAssigned),
		attribute.Int("instance_start.start_retries", m.StartRetries),
		attribute.Int("instance_start.heartbeat_retries", m.HeartbeatRetries),
		attribute.String("instance_start.heartbeat_took", m.HeartbeatTook.String()),
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
	QueueWaitTook        time.Duration
	GuestInvokeTook      time.Duration
	ResponseStatusCode   uint32
	ResponseBodyBytes    int
}

type GuestInvokeMeta struct {
	Method             string
	Path               string
	RequestBodyBytes   int
	HeadersCount       int
	InvocationTook     time.Duration
	ResponseStatusCode int
	ResponseBodyBytes  int
}

func (m GuestInvokeMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m GuestInvokeMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("guest.invoke.method", m.Method),
		attribute.String("guest.invoke.path", m.Path),
		attribute.Int("guest.invoke.request_body_bytes", m.RequestBodyBytes),
		attribute.Int("guest.invoke.headers_count", m.HeadersCount),
		attribute.String("guest.invoke.took", m.InvocationTook.String()),
		attribute.Int("guest.invoke.response_status_code", m.ResponseStatusCode),
		attribute.Int("guest.invoke.response_body_bytes", m.ResponseBodyBytes),
	}
}

func (m InvokeMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m InvokeMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("invoke.instance_id", m.InstanceID),
		attribute.String("invoke.func.path", m.FunctionPath),
		attribute.String("invoke.method", m.Method),
		attribute.String("invoke.path", m.Path),
		attribute.Int("invoke.request_body_bytes", m.RequestBodyBytes),
		attribute.Int("invoke.headers_count", m.HeadersCount),
		attribute.Bool("invoke.worker_already_running", m.WorkerAlreadyRunning),
		attribute.Bool("invoke.started_worker", m.StartedWorker),
		attribute.Int("invoke.queue_depth_at_enqueue", m.QueueDepthAtEnqueue),
		attribute.String("invoke.took", m.InvocationTook.String()),
		attribute.String("invoke.queue_wait_took", m.QueueWaitTook.String()),
		attribute.String("invoke.guest_invoke_took", m.GuestInvokeTook.String()),
		attribute.Int64("invoke.response_status_code", int64(m.ResponseStatusCode)),
		attribute.Int("invoke.response_body_bytes", m.ResponseBodyBytes),
	}
}

type RouteFunctionMeta struct {
	FunctionID       string
	RunnerAddr       string
	InstanceID       string
	RouteTook        time.Duration
	CacheHit         bool
	PreparedInstance bool
	PrepareTook      time.Duration
}

func (m RouteFunctionMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m RouteFunctionMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("route_function.function_id", m.FunctionID),
		attribute.String("route_function.runner_addr", m.RunnerAddr),
		attribute.String("route_function.instance_id", m.InstanceID),
		attribute.String("route_function.took", m.RouteTook.String()),
		attribute.Bool("route_function.cache_hit", m.CacheHit),
		attribute.Bool("route_function.prepared_instance", m.PreparedInstance),
		attribute.String("route_function.prepare_took", m.PrepareTook.String()),
	}
}

type RegisterRunnerMeta struct {
	RunnerAddr        string
	RunnerID          int64
	PreexistingRunner bool
	RunnerHeartbeatOK bool
	RegistrationTook  time.Duration
	HeartbeatTook     time.Duration
	PersistTook       time.Duration
}

func (m RegisterRunnerMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m RegisterRunnerMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("register_runner.addr", m.RunnerAddr),
		attribute.Int64("register_runner.id", m.RunnerID),
		attribute.Bool("register_runner.preexisting", m.PreexistingRunner),
		attribute.Bool("register_runner.heartbeat_ok", m.RunnerHeartbeatOK),
		attribute.String("register_runner.took", m.RegistrationTook.String()),
		attribute.String("register_runner.heartbeat_took", m.HeartbeatTook.String()),
		attribute.String("register_runner.persist_took", m.PersistTook.String()),
	}
}

func fieldsFromAttributes(attrs ...attribute.KeyValue) logging.Fields {
	if len(attrs) == 0 {
		return nil
	}

	fields := make(logging.Fields, 0, len(attrs)*2)
	for _, attr := range attrs {
		fields = append(fields, string(attr.Key), attr.Value.AsInterface())
	}
	return fields
}

func contextAttributes(ctx context.Context) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 32)

	if downloadMeta := GetDownloadMeta(ctx); downloadMeta != nil {
		attrs = append(attrs, downloadMeta.Attributes()...)
	}

	if guestInvokeMeta := GetGuestInvokeMeta(ctx); guestInvokeMeta != nil {
		attrs = append(attrs, guestInvokeMeta.Attributes()...)
	}

	if instanceStartMeta := GetInstanceStartMeta(ctx); instanceStartMeta != nil {
		attrs = append(attrs, instanceStartMeta.Attributes()...)
	}

	if invokeMeta := GetInvokeMeta(ctx); invokeMeta != nil {
		attrs = append(attrs, invokeMeta.Attributes()...)
	}

	if routeFunctionMeta := GetRouteFunctionMeta(ctx); routeFunctionMeta != nil {
		attrs = append(attrs, routeFunctionMeta.Attributes()...)
	}

	if registerRunnerMeta := GetRegisterRunnerMeta(ctx); registerRunnerMeta != nil {
		attrs = append(attrs, registerRunnerMeta.Attributes()...)
	}

	return attrs
}

func contextFields(ctx context.Context) logging.Fields {
	fields := logging.Fields{}

	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		fields = append(fields,
			"trace_id", span.TraceID().String(),
			"span_id", span.SpanID().String(),
		)
	}

	attrs := contextAttributes(ctx)
	if len(fields) == 0 && len(attrs) == 0 {
		return nil
	}

	fields = append(fields, fieldsFromAttributes(attrs...)...)
	return fields
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

func WithGuestInvokeMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &guestInvokeMetaHolder{}
	ctx = context.WithValue(ctx, guestInvokeMetaKey{}, holder)
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

func WithRouteFunctionMetaHolder(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	holder := &routeFunctionMetaHolder{}
	ctx = context.WithValue(ctx, routeFunctionMetaKey{}, holder)
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

func GetGuestInvokeMeta(ctx context.Context) *GuestInvokeMeta {
	holder, ok := ctx.Value(guestInvokeMetaKey{}).(*guestInvokeMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetGuestInvokeMeta(ctx context.Context, meta *GuestInvokeMeta) {
	holder, ok := ctx.Value(guestInvokeMetaKey{}).(*guestInvokeMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
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

func GetRouteFunctionMeta(ctx context.Context) *RouteFunctionMeta {
	holder, ok := ctx.Value(routeFunctionMetaKey{}).(*routeFunctionMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetRouteFunctionMeta(ctx context.Context, meta *RouteFunctionMeta) {
	holder, ok := ctx.Value(routeFunctionMetaKey{}).(*routeFunctionMetaHolder)
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
