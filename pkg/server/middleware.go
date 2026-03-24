package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	otelchimetric "github.com/riandyrn/otelchi/metric"
)

const HeartbeatEndpoint = "/monitoring/heartbeat"

func HttpMiddleware(
	otelCfg otelchimetric.BaseConfig,
	loggingOpts MonitoringOpts,
) []func(http.Handler) http.Handler {
	ms := []func(http.Handler) http.Handler{
		WithAPIErrorHolder,
		CreateHTTPLogger(loggingOpts),
		middleware.Recoverer,
	}

	if loggingOpts.OTELOn {
		ms = append(ms,
			otelchimetric.NewRequestDurationMillis(otelCfg),
			otelchimetric.NewRequestInFlight(otelCfg),
			otelchimetric.NewResponseSizeBytes(otelCfg),
		)
	}
	return ms
}

type (
	apiErrorKey    struct{}
	apiErrorHolder struct {
		Err *ApiError
	}
)

func WithAPIErrorHolder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		holder := &apiErrorHolder{}
		ctx := context.WithValue(r.Context(), apiErrorKey{}, holder)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetAPIError(ctx context.Context) *ApiError {
	holder, ok := ctx.Value(apiErrorKey{}).(*apiErrorHolder)
	if !ok {
		slog.Error("api error holder in request context has unexpected type or is absent")
		return nil
	} else if holder == nil {
		slog.Error("api error holder in request context is nil")
		return nil
	}
	return holder.Err
}

func SetAPIError(r *http.Request, err *ApiError) {
	holder, ok := r.Context().Value(apiErrorKey{}).(*apiErrorHolder)
	if !ok {
		slog.Error("api error holder in request context has unexpected type or is absent")
		return
	} else if holder == nil {
		slog.Error("api error holder in request context is nil")
		return
	}
	holder.Err = err
}
