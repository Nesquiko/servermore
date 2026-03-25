package server

import (
	"log/slog"

	"github.com/Nesquiko/servermore/pkg/assert"
	grpc "google.golang.org/grpc"
)

func CloseConn(conn *grpc.ClientConn, logAttrs ...any) {
	assert.That(conn != nil, "caller send nil connection")
	if err := conn.Close(); err != nil {
		logAttrs = append(logAttrs, slog.Any("error", err))
		slog.Error("closing client connection failed", logAttrs...)
	}
}
