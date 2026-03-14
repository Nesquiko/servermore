package server

import (
	"context"
	"errors"
	"fmt"

	otelchimetric "github.com/riandyrn/otelchi/metric"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type Opts struct {
	ExportEnabled bool

	AppName    string
	AppVersion string
	Env        string
}

func InitOTEL(
	ctx context.Context,
	opts Opts,
) (otelchimetric.BaseConfig, func(ctx context.Context) error, error) {
	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(opts.AppName),
			semconv.ServiceVersionKey.String(opts.AppVersion),
			semconv.DeploymentEnvironmentName(opts.Env),
		),
	)
	if err != nil {
		return otelchimetric.BaseConfig{}, nil, fmt.Errorf(
			"InitOTEL failed to initialize resource: %w",
			err,
		)
	}

	tp, err := InitTracerProvider(ctx, res, opts)
	if err != nil {
		return otelchimetric.BaseConfig{}, nil, fmt.Errorf(
			"InitOTEL failed to initialize tracer provider: %w",
			err,
		)
	}
	mp, err := InitMeter(ctx, res, opts)
	if err != nil {
		return otelchimetric.BaseConfig{}, nil, fmt.Errorf(
			"InitOTEL failed to initialize meter provider: %w",
			err,
		)
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
	baseCfg := otelchimetric.NewBaseConfig(opts.AppName, otelchimetric.WithMeterProvider(mp))

	return baseCfg, closer, nil
}

func InitTracerProvider(
	ctx context.Context,
	res *resource.Resource,
	opts Opts,
) (*sdktrace.TracerProvider, error) {
	tracerOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
	}

	if opts.ExportEnabled {
		exporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("InitTracerProvider failed to initialize exporter: %w", err)
		}
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(exporter))
	}

	return sdktrace.NewTracerProvider(tracerOpts...), nil
}

func InitMeter(
	ctx context.Context,
	res *resource.Resource,
	opts Opts,
) (*sdkmetric.MeterProvider, error) {
	providerOpts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}

	if opts.ExportEnabled {
		exporter, err := otlpmetricgrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("InitMeter failed to initialize exporter: %w", err)
		}
		providerOpts = append(
			providerOpts,
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		)
	}

	return sdkmetric.NewMeterProvider(providerOpts...), nil
}
