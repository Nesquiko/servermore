package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type (
	createFunctionMetaKey         struct{}
	downloadFunctionBinaryMetaKey struct{}
	gatewayFunctionRequestMetaKey struct{}

	createFunctionMetaHolder struct {
		Meta *CreateFunctionMeta
	}

	downloadFunctionBinaryMetaHolder struct {
		Meta *DownloadFunctionBinaryMeta
	}

	gatewayFunctionRequestMetaHolder struct {
		Meta *GatewayFunctionRequestMeta
	}
)

type CreateFunctionMeta struct {
	FunctionName          string
	FunctionID            int64
	FunctionBytes         int
	FunctionHash          string
	FunctionPath          string
	FunctionAlreadyExists bool
}

func (m CreateFunctionMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m CreateFunctionMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("function.name", m.FunctionName),
		attribute.Int64("function.id", m.FunctionID),
		attribute.Int("function.bytes", m.FunctionBytes),
		attribute.String("function.hash", m.FunctionHash),
		attribute.String("function.path", m.FunctionPath),
		attribute.Bool("function.alread_exists", m.FunctionAlreadyExists),
	}
}

type DownloadFunctionBinaryMeta struct {
	FunctionID       int64
	FunctionPath     string
	FunctionFilename string
	BytesWritten     int64
}

func (m DownloadFunctionBinaryMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m DownloadFunctionBinaryMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int64("download.function_id", m.FunctionID),
		attribute.String("download.function_path", m.FunctionPath),
		attribute.String("download.function_filename", m.FunctionFilename),
		attribute.Int64("download.bytes_written", m.BytesWritten),
	}
}

type GatewayFunctionRequestMeta struct {
	FunctionID         string
	RequestMethod      string
	RequestPath        string
	ForwardedPath      string
	RequestBodyBytes   int
	HeadersCount       int
	RunnerAddr         string
	InstanceID         string
	RunnerConnReused   bool
	RouteTook          time.Duration
	InvokeTook         time.Duration
	ResponseStatusCode int
	ResponseBodyBytes  int
}

func (m GatewayFunctionRequestMeta) Fields() logging.Fields {
	return fieldsFromAttributes(m.Attributes()...)
}

func (m GatewayFunctionRequestMeta) Attributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("gateway.function_id", m.FunctionID),
		attribute.String("gateway.request_method", m.RequestMethod),
		attribute.String("gateway.request_path", m.RequestPath),
		attribute.String("gateway.forwarded_path", m.ForwardedPath),
		attribute.Int("gateway.request_body_bytes", m.RequestBodyBytes),
		attribute.Int("gateway.headers_count", m.HeadersCount),
		attribute.String("gateway.runner_addr", m.RunnerAddr),
		attribute.String("gateway.instance_id", m.InstanceID),
		attribute.Bool("gateway.runner_conn_reused", m.RunnerConnReused),
		attribute.String("gateway.route_took", m.RouteTook.String()),
		attribute.String("gateway.invoke_took", m.InvokeTook.String()),
		attribute.Int("gateway.response_status_code", m.ResponseStatusCode),
		attribute.Int("gateway.response_body_bytes", m.ResponseBodyBytes),
	}
}

func httpContextFields(ctx context.Context) logging.Fields {
	fields := logging.Fields{}

	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		fields = append(fields,
			"trace_id", span.TraceID().String(),
			"span_id", span.SpanID().String(),
		)
	}

	if downloadMeta := GetDownloadFunctionBinaryMeta(ctx); downloadMeta != nil {
		fields = append(fields, downloadMeta.Fields()...)
	}

	if createMeta := GetCreateFunctionMeta(ctx); createMeta != nil {
		fields = append(fields, createMeta.Fields()...)
	}

	if gatewayMeta := GetGatewayFunctionRequestMeta(ctx); gatewayMeta != nil {
		fields = append(fields, gatewayMeta.Fields()...)
	}

	if len(fields) == 0 {
		return nil
	}

	return fields
}

func httpContextAttrs(ctx context.Context) []slog.Attr {
	return slogAttrsFromFields(httpContextFields(ctx))
}

func slogAttrsFromFields(fields logging.Fields) []slog.Attr {
	if len(fields) == 0 {
		return nil
	}

	attrs := make([]slog.Attr, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			continue
		}
		attrs = append(attrs, slog.Any(key, fields[i+1]))
	}

	if len(attrs) == 0 {
		return nil
	}

	return attrs
}

func WithDownloadFunctionBinaryMetaHolder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		holder := &downloadFunctionBinaryMetaHolder{}
		ctx := context.WithValue(r.Context(), downloadFunctionBinaryMetaKey{}, holder)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func WithCreateFunctionMetaHolder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		holder := &createFunctionMetaHolder{}
		ctx := context.WithValue(r.Context(), createFunctionMetaKey{}, holder)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func WithGatewayFunctionRequestMetaHolder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		holder := &gatewayFunctionRequestMetaHolder{}
		ctx := context.WithValue(r.Context(), gatewayFunctionRequestMetaKey{}, holder)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetDownloadFunctionBinaryMeta(ctx context.Context) *DownloadFunctionBinaryMeta {
	holder, ok := ctx.Value(downloadFunctionBinaryMetaKey{}).(*downloadFunctionBinaryMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetDownloadFunctionBinaryMeta(ctx context.Context, meta *DownloadFunctionBinaryMeta) {
	holder, ok := ctx.Value(downloadFunctionBinaryMetaKey{}).(*downloadFunctionBinaryMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}

func GetCreateFunctionMeta(ctx context.Context) *CreateFunctionMeta {
	holder, ok := ctx.Value(createFunctionMetaKey{}).(*createFunctionMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetCreateFunctionMeta(ctx context.Context, meta *CreateFunctionMeta) {
	holder, ok := ctx.Value(createFunctionMetaKey{}).(*createFunctionMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}

func GetGatewayFunctionRequestMeta(ctx context.Context) *GatewayFunctionRequestMeta {
	holder, ok := ctx.Value(gatewayFunctionRequestMetaKey{}).(*gatewayFunctionRequestMetaHolder)
	if !ok {
		return nil
	} else if holder == nil {
		return nil
	}

	return holder.Meta
}

func SetGatewayFunctionRequestMeta(ctx context.Context, meta *GatewayFunctionRequestMeta) {
	holder, ok := ctx.Value(gatewayFunctionRequestMetaKey{}).(*gatewayFunctionRequestMetaHolder)
	if !ok {
		return
	} else if holder == nil {
		return
	}

	holder.Meta = meta
}
