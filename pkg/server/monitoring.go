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
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type MonitoringOpts struct {
	// Env value: PROD | TEST | LOCAL
	Env string

	AppName         string
	AppVersion      string
	AdditionalAttrs map[string]string

	Level  slog.Level
	OTELOn bool
}

func (o MonitoringOpts) IsDev() bool {
	return o.Env != "PROD"
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

	if opts.OTELOn {
		exporter, err := otlptracegrpc.New(ctx)
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(exporter))
		if err != nil {
			return nil, fmt.Errorf("failed to initialize exporter: %w", err)
		}
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

	if opts.OTELOn {
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
	logFmt := logSchema.Concise(opts.IsDev())

	logger := slogLogger(
		opts,
		&slog.HandlerOptions{ReplaceAttr: logFmt.ReplaceAttr, Level: opts.Level},
	)

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

func InstrumentedGrpcServer(
	opts MonitoringOpts,
	interceptors ...grpc.UnaryServerInterceptor,
) *grpc.Server {
	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(interceptors...),
		grpc.ChainUnaryInterceptor(grpcServerLogger(opts)),
	)
}

func LoggingGrpcClient(
	addr string,
	opts MonitoringOpts,
) (conn *grpc.ClientConn, err error) {
	return grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(grpcClientLogger(opts)),
	)
}

func GrpcClient(addr string) (conn *grpc.ClientConn, err error) {
	return grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
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
	logger := slogLogger(opts, &slog.HandlerOptions{Level: opts.Level})

	grpcLoggingOpts := []logging.Option{
		logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
		logging.WithDurationField(logging.DurationToDurationField),
		logging.WithFieldsFromContext(func(ctx context.Context) logging.Fields {
			fields := logging.Fields{}

			if span := trace.SpanContextFromContext(ctx); span.IsSampled() {
				return logging.Fields{"trace_id", span.TraceID().String()}
			}

			downloadMeta := GetDownloadMeta(ctx)
			if downloadMeta != nil {
				fields = append(fields, downloadMeta.Fields()...)
			}

			instanceStartMeta := GetInstanceStartMeta(ctx)
			if instanceStartMeta != nil {
				fields = append(fields, instanceStartMeta.Fields()...)
			}

			invokeMeta := GetInvokeMeta(ctx)
			if invokeMeta != nil {
				fields = append(fields, invokeMeta.Fields()...)
			}

			registerRunnerMeta := GetRegisterRunnerMeta(ctx)
			if registerRunnerMeta != nil {
				fields = append(fields, registerRunnerMeta.Fields()...)
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
	logger := slog.
		New(slog.NewTextHandler(os.Stdout, slogOpts)).
		With(
			slog.String("app", opts.AppName),
			slog.String("env", opts.Env),
		)

	if opts.AppVersion != "" {
		logger = logger.With(slog.String("version", opts.AppVersion))
	}

	if opts.AdditionalAttrs != nil {
		for k, v := range opts.AdditionalAttrs {
			logger = logger.With(slog.String(k, v))
		}
	}
	slog.SetDefault(logger)

	return logger
}
