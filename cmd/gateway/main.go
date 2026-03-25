package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const FunctionIdPathParam = "functionId"

func main() {
	ctx := context.Background()
	opts := server.MonitoringOpts{
		AppName:    "gateway",
		AppVersion: "0.0.1",
		Env:        "LOCAL",
	}
	otelCfg, shutdown, err := server.InitHttpOTEL(ctx, opts)
	if err != nil {
		slog.Error("failed to initialize OTEL", "error", err)
		return
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			slog.Error("error during shutdown", "error", err)
		}
	}()

	r := chi.NewRouter()
	baseUrl := fmt.Sprintf("/{%s}", FunctionIdPathParam)

	r.Use(middleware.Heartbeat(server.HeartbeatEndpoint))
	r.Use(server.HttpMiddleware(otelCfg, opts)...)
	r.Route(baseUrl, func(r chi.Router) {
		r.Post("/", processFunctionRequest)
		r.Post("/*", processFunctionRequest)
	})

	if err := http.ListenAndServe(":42069", r); err != nil && err != http.ErrServerClosed {
		slog.Error("gateway failed with error", "error", err)
	}
}

func processFunctionRequest(w http.ResponseWriter, r *http.Request) {
	functionId := chi.URLParam(r, FunctionIdPathParam)
	rest := chi.URLParam(r, "*")

	_, err := fmt.Fprintf(
		w,
		"hi, you are calling function '%s' with endpoint '%s'",
		functionId, rest,
	)
	if err != nil {
		slog.Error("processFunctionRequest errored when encoding response", "error", err)
		server.InternalServerError(w, r, err)
	}
}
