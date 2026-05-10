package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"

	"github.com/go-chi/httplog/v3"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	otelchimetric "github.com/riandyrn/otelchi/metric"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
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
	"google.golang.org/grpc/stats"
)

type MonitoringOpts struct {
	Env Environment `yaml:"env"`

	AppName         string            `yaml:"app_name"`
	AppVersion      string            `yaml:"app_version"`
	AdditionalAttrs map[string]string `yaml:"additional_attrs"`

	Level  slog.Level `yaml:"level"`
	OTELOn bool       `yaml:"otel_on"`
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
			semconv.DeploymentEnvironmentName(string(opts.Env)),
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
			attrs := httpContextAttrs(req.Context())

			apiErr := GetAPIError(req.Context())
			if apiErr == nil {
				if len(attrs) == 0 {
					return nil
				}
				return attrs
			}

			attrs = append(attrs,
				slog.String("error", apiErr.cause.Error()),
				slog.String("api.error.code", apiErr.Code),
				slog.Int("api.error.status", apiErr.Status),
				slog.String("api.error.instance", apiErr.Instance),
				slog.String("api.error.title", apiErr.Title),
				slog.String("api.error.detail", apiErr.Detail),
			)

			return attrs
		},
	})
}

func InstrumentedGrpcServer(
	opts MonitoringOpts,
	interceptors ...grpc.UnaryServerInterceptor,
) *grpc.Server {
	return InstrumentedGrpcServerWithExcludedMethodLogs(opts, nil, interceptors...)
}

func InstrumentedGrpcServerWithExcludedMethodLogs(
	opts MonitoringOpts,
	logExcludedMethods []string,
	interceptors ...grpc.UnaryServerInterceptor,
) *grpc.Server {
	interceptors = append(
		interceptors,
		grpcServerTraceInterceptor(logExcludedMethods),
		grpcServerLogger(opts, logExcludedMethods),
	)
	return grpc.NewServer(
		grpc.StatsHandler(
			otelgrpc.NewServerHandler(otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool {
				return grpcMethodAllowed(info.FullMethodName, logExcludedMethods)
			})),
		),
		grpc.MaxRecvMsgSize(GrpcMaxBytes),
		grpc.MaxSendMsgSize(GrpcMaxBytes),
		grpc.ChainUnaryInterceptor(interceptors...),
	)
}

func LoggingGrpcClient(
	addr string,
	opts MonitoringOpts,
) (conn *grpc.ClientConn, err error) {
	return LoggingGrpcClientWithExcludedMethodLogs(addr, opts, nil)
}

func LoggingGrpcClientWithExcludedMethodLogs(
	addr string,
	opts MonitoringOpts,
	logExcludedMethods []string,
) (conn *grpc.ClientConn, err error) {
	return grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(
			otelgrpc.NewClientHandler(otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool {
				return grpcMethodAllowed(info.FullMethodName, logExcludedMethods)
			})),
		),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(GrpcMaxBytes),
			grpc.MaxCallSendMsgSize(GrpcMaxBytes),
		),
		grpc.WithChainUnaryInterceptor(
			grpcClientTraceInterceptor(logExcludedMethods),
			grpcClientLogger(opts, logExcludedMethods),
		),
	)
}

func GrpcClient(addr string) (conn *grpc.ClientConn, err error) {
	return grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(GrpcMaxBytes),
			grpc.MaxCallSendMsgSize(GrpcMaxBytes),
		),
		grpc.WithChainUnaryInterceptor(grpcClientTraceInterceptor(nil)),
	)
}

func grpcServerLogger(
	opts MonitoringOpts,
	logExcludedMethods []string,
) grpc.UnaryServerInterceptor {
	grpcInterceptor, grpcLoggingOpts := grpcLogger(opts)
	interceptor := logging.UnaryServerInterceptor(grpcInterceptor, grpcLoggingOpts...)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		if !grpcMethodAllowed(info.FullMethod, logExcludedMethods) {
			return handler(ctx, req)
		}
		return interceptor(ctx, req, info, handler)
	}
}

func grpcClientLogger(
	opts MonitoringOpts,
	logExcludedMethods []string,
) grpc.UnaryClientInterceptor {
	grpcInterceptor, grpcLoggingOpts := grpcLogger(opts)
	interceptor := logging.UnaryClientInterceptor(grpcInterceptor, grpcLoggingOpts...)
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if !grpcMethodAllowed(method, logExcludedMethods) {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		return interceptor(ctx, method, req, reply, cc, invoker, opts...)
	}
}

func grpcLogger(opts MonitoringOpts) (logging.LoggerFunc, []logging.Option) {
	logger := slogLogger(opts, &slog.HandlerOptions{Level: opts.Level})

	grpcLoggingOpts := []logging.Option{
		logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
		logging.WithDurationField(logging.DurationToDurationField),
		logging.WithFieldsFromContext(func(ctx context.Context) logging.Fields {
			return contextFields(ctx)
		}),
	}

	// grpcInterceptorLogger adapts slog logger to interceptor logger.
	grpcInterceptor := logging.LoggerFunc(
		func(ctx context.Context, lvl logging.Level, msg string, fields ...any) {
			logger.Log(ctx, slog.Level(lvl), msg, fields...)
		})

	return grpcInterceptor, grpcLoggingOpts
}

func grpcServerTraceInterceptor(logExcludedMethods []string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		if !grpcMethodAllowed(info.FullMethod, logExcludedMethods) {
			return handler(ctx, req)
		}
		resp, err = handler(ctx, req)
		annotateSpan(ctx, err)
		return resp, err
	}
}

func grpcClientTraceInterceptor(logExcludedMethods []string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		if !grpcMethodAllowed(method, logExcludedMethods) {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		err = invoker(ctx, method, req, reply, cc, opts...)
		annotateSpan(ctx, err)
		return err
	}
}

func grpcMethodAllowed(method string, excludedMethods []string) bool {
	return !slices.Contains(excludedMethods, method)
}

func annotateSpan(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	attrs := contextAttributes(ctx)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

func slogLogger(opts MonitoringOpts, slogOpts *slog.HandlerOptions) *slog.Logger {
	logger := slog.
		New(slog.NewTextHandler(os.Stdout, slogOpts)).
		With(
			slog.String("app", opts.AppName),
			slog.String("env", string(opts.Env)),
		)

	if opts.AppVersion != "" {
		logger = logger.With(slog.String("version", opts.AppVersion))
	}

	if opts.AdditionalAttrs != nil {
		for k, v := range opts.AdditionalAttrs {
			logger = logger.With(slog.String(k, v))
		}
	}

	return logger
}

func SetDefaultLogger(opts MonitoringOpts) {
	slog.SetDefault(slogLogger(opts, &slog.HandlerOptions{Level: opts.Level}))
}
