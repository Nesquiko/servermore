package server

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/httplog/v3"
)

func CreateLogger(opts Opts) func(http.Handler) http.Handler {
	logSchema := httplog.SchemaOTEL
	logFmt := logSchema.Concise(opts.ExportEnabled)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: logFmt.ReplaceAttr,
	})).With(
		slog.String("app", opts.AppName),
		slog.String("version", opts.AppVersion),
		slog.String("env", opts.Env),
	)

	return httplog.RequestLogger(logger, &httplog.Options{
		Level:             slog.LevelInfo,
		Schema:            logSchema,
		RecoverPanics:     true,
		LogRequestHeaders: []string{"Origin"},
	})
}
