package commander

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	otelchimetric "github.com/riandyrn/otelchi/metric"
)

func Run(ctx context.Context, conf CommanderHTTPServerConfig) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := NewCommanderServer(conf)
	if err != nil {
		slog.Error("failed to initialize server", "error", err)
		return fmt.Errorf("server initialization failed: %w", err)
	}

	monitoringOpts := server.MonitoringOpts{
		IsDev:      conf.Env != "PROD",
		AppName:    conf.AppName,
		AppVersion: conf.CommitHash,
		Env:        conf.Env,
	}

	otelCfg, otelShutdown, err := server.InitHttpOTEL(ctx, monitoringOpts)
	if err != nil {
		slog.Error("failed to initialize OTEL", "error", err)
		return fmt.Errorf("OTEL initialization failed: %w", err)
	}

	r := chi.NewMux()
	r.Use(middleware.Heartbeat(server.HeartbeatEndpoint))

	handler := api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseURL:          conf.BaseURL,
		BaseRouter:       r,
		Middlewares:      createMiddleware(otelCfg, monitoringOpts),
		ErrorHandlerFunc: server.ErrorHandlerFunc,
	})

	httpServer := &http.Server{
		Addr:    net.JoinHostPort(conf.Host, conf.Port),
		Handler: handler,
	}

	errCh := make(chan error, 1)

	go func() {
		slog.Info("starting server", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("interrupt received, shutting down server")
	case err := <-errCh:
		if err != nil {
			slog.Error("http server failed", "error", err)
			return fmt.Errorf("http server failed: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	return errors.Join(
		httpServer.Shutdown(shutdownCtx),
		otelShutdown(shutdownCtx),
	)
}

func createMiddleware(
	otelCfg otelchimetric.BaseConfig,
	loggingOpts server.MonitoringOpts,
) []api.MiddlewareFunc {
	ms := server.HttpMiddleware(otelCfg, loggingOpts)
	out := make([]api.MiddlewareFunc, len(ms))
	for i, m := range ms {
		out[i] = api.MiddlewareFunc(m)
	}
	return out
}
