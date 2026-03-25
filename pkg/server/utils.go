package server

import (
	"context"
	"io"
	"log/slog"

	"github.com/Nesquiko/servermore/pkg/assert"
)

func Close(c io.Closer, logAttrs ...any) {
	assert.That(c != nil, "caller send nil closer")
	if err := c.Close(); err != nil {
		logAttrs = append(logAttrs, slog.Any("error", err))
		slog.Error("closing failed", logAttrs...)
	}
}

func CloseWithCtx(ctx context.Context, closer func(ctx context.Context) error, logAttrs ...any) {
	assert.That(closer != nil, "caller send nil closer")
	if err := closer(ctx); err != nil {
		logAttrs = append(logAttrs, slog.Any("error", err))
		slog.Error("closing with context failed", logAttrs...)
	}
}
