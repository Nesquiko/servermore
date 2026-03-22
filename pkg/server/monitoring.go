package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/httplog/v3"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	otelchimetric "github.com/riandyrn/otelchi/metric"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type MonitoringOpts struct {
	IsDev bool

	AppName    string
	AppVersion string
	// Env value: PROD | TEST | LOCAL
	Env string
}

func InitHttpOTEL(
	ctx context.Context,
	opts MonitoringOpts,
) (otelchimetric.BaseConfig, func(ctx context.Context) error, error) {
	mp, closer, err := InitOTEL(ctx, opts)
	if err != nil {
		return otelchimetric.BaseConfig{}, nil, fmt.Errorf("OTEL initialization failed: %w", err)
	}

	baseCfg := otelchimetric.NewBaseConfig(opts.AppName, otelchimetric.WithMeterProvider(mp))
	return baseCfg, closer, nil
}

func InitOTEL(
	ctx context.Context,
	opts MonitoringOpts,
) (*sdkmetric.MeterProvider, func(ctx context.Context) error, error) {
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(opts.AppName),
			semconv.ServiceVersionKey.String(opts.AppVersion),
			semconv.DeploymentEnvironmentName(opts.Env),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize resource: %w", err)
	}

	tp, err := InitTracerProvider(ctx, res, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize tracer provider: %w", err)
	}
	mp, err := InitMeter(ctx, res, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize meter provider: %w", err)
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	closer := func(ctx context.Context) error {
		return errors.Join(
			mp.Shutdown(ctx),
			tp.Shutdown(ctx),
		)
	}

	return mp, closer, nil
}

func InitTracerProvider(
	ctx context.Context,
	res *resource.Resource,
	opts MonitoringOpts,
) (*sdktrace.TracerProvider, error) {
	tracerOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
	}

	if opts.IsDev {
		exporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize exporter: %w", err)
		}
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(exporter))
	}

	return sdktrace.NewTracerProvider(tracerOpts...), nil
}

func InitMeter(
	ctx context.Context,
	res *resource.Resource,
	opts MonitoringOpts,
) (*sdkmetric.MeterProvider, error) {
	providerOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}

	if opts.IsDev {
		exporter, err := otlpmetricgrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize exporter: %w", err)
		}
		providerOpts = append(
			providerOpts,
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		)
	}

	return sdkmetric.NewMeterProvider(providerOpts...), nil
}

func CreateHTTPLogger(opts MonitoringOpts) func(http.Handler) http.Handler {
	logSchema := httplog.SchemaOTEL
	logFmt := logSchema.Concise(opts.IsDev)

	logger := slogLogger(opts, &slog.HandlerOptions{ReplaceAttr: logFmt.ReplaceAttr})

	return httplog.RequestLogger(logger, &httplog.Options{
		Level:             slog.LevelInfo,
		Schema:            logSchema,
		RecoverPanics:     true,
		LogRequestHeaders: []string{"Origin"},
		LogExtraAttrs: func(req *http.Request, _ string, _ int) []slog.Attr {
			apiErr := GetAPIError(req.Context())
			if apiErr == nil {
				return nil
			}

			attrs := []slog.Attr{
				slog.String("error", apiErr.cause.Error()),
				slog.String("api.error.code", apiErr.Code),
				slog.Int("api.error.status", apiErr.Status),
				slog.String("api.error.instance", apiErr.Instance),
				slog.String("api.error.title", apiErr.Title),
				slog.String("api.error.detail", apiErr.Detail),
			}

			return attrs
		},
	})
}

func InstrumentedGrpcServer(opts MonitoringOpts) *grpc.Server {
	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			WithDownloadMetaHolder,
			WithInstanceStartMetaHolder,
			WithInvokeMetaHolder,
			grpcServerLogger(opts),
		),
	)
}

func LoggingGrpcClient(addr string, opts MonitoringOpts) (conn *grpc.ClientConn, err error) {
	return grpc.NewClient(
		addr,
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(grpcClientLogger(opts)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func GrpcClient(addr string) (conn *grpc.ClientConn, err error) {
	return grpc.NewClient(
		addr,
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func grpcServerLogger(opts MonitoringOpts) grpc.UnaryServerInterceptor {
	grpcInterceptor, grpcLoggingOpts := grpcLogger(opts)
	return logging.UnaryServerInterceptor(grpcInterceptor, grpcLoggingOpts...)
}

func grpcClientLogger(opts MonitoringOpts) grpc.UnaryClientInterceptor {
	grpcInterceptor, grpcLoggingOpts := grpcLogger(opts)
	return logging.UnaryClientInterceptor(grpcInterceptor, grpcLoggingOpts...)
}

func grpcLogger(opts MonitoringOpts) (logging.LoggerFunc, []logging.Option) {
	logger := slogLogger(opts, nil)

	grpcLoggingOpts := []logging.Option{
		logging.WithLogOnEvents(logging.FinishCall),
		logging.WithDurationField(logging.DurationToDurationField),
		logging.WithFieldsFromContext(func(ctx context.Context) logging.Fields {
			fields := logging.Fields{}

			downloadMeta := GetDownloadMeta(ctx)
			if downloadMeta != nil {
				fields = append(fields,
					"download.function_id", downloadMeta.FunctionID,
					"download.downloaded", downloadMeta.Downloaded,
					"download.download_path", downloadMeta.DownloadPath,
					"download.stored_path", downloadMeta.StoredPath,
					"download.bytes_written", downloadMeta.BytesWritten,
					"download.took", downloadMeta.DownloadTook,
					"download.reused_from_fs", downloadMeta.ReusedFromFS,
					"download.reused_inflight", downloadMeta.ReusedInFlight,
				)
			}

			instanceStartMeta := GetInstanceStartMeta(ctx)
			if instanceStartMeta != nil {
				fields = append(fields,
					"instance_start.function_path", instanceStartMeta.FunctionPath,
					"instance_start.instance_id", instanceStartMeta.InstanceID,
					"instance_start.runtime_type", instanceStartMeta.RuntimeType,
					"instance_start.instance_addr", instanceStartMeta.InstanceAddr,
					"instance_start.start_took", instanceStartMeta.StartTook,
					"instance_start.reused_assigned", instanceStartMeta.ReusedAssigned,
				)
			}

			invokeMeta := GetInvokeMeta(ctx)
			if invokeMeta != nil {
				fields = append(fields,
					"invoke.instance_id", invokeMeta.InstanceID,
					"invoke.method", invokeMeta.Method,
					"invoke.path", invokeMeta.Path,
					"invoke.request_body_bytes", invokeMeta.RequestBodyBytes,
					"invoke.headers_count", invokeMeta.HeadersCount,
					"invoke.worker_already_running", invokeMeta.WorkerAlreadyRunning,
					"invoke.started_worker", invokeMeta.StartedWorker,
					"invoke.queue_depth_at_enqueue", invokeMeta.QueueDepthAtEnqueue,
					"invoke.took", invokeMeta.InvocationTook,
					"invoke.response_status_code", invokeMeta.ResponseStatusCode,
					"invoke.response_body_bytes", invokeMeta.ResponseBodyBytes,
				)
			}

			if len(fields) == 0 {
				return nil
			}

			return fields
		}),
	}

	// grpcInterceptorLogger adapts slog logger to interceptor logger.
	grpcInterceptor := logging.LoggerFunc(
		func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
			logger.Log(ctx, slog.Level(lvl), msg, fields...)
		})

	return grpcInterceptor, grpcLoggingOpts
}

func slogLogger(opts MonitoringOpts, slogOpts *slog.HandlerOptions) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, slogOpts)).With(
		slog.String("app", opts.AppName),
		slog.String("version", opts.AppVersion),
		slog.String("env", opts.Env),
	)
}
