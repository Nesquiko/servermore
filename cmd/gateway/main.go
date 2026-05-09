package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Nesquiko/servermore/pkg/gateway"
	"github.com/Nesquiko/servermore/pkg/server"
)

const FunctionIdPathParam = "functionId"

func main() {
	ctx := context.Background()

	cfg := gateway.GatewayConfig{
		AppName:                       "gateway",
		Env:                           server.LOCAL,
		Address:                       ":42069",
		CommanderClientMonitoringOpts: server.MonitoringOpts{Env: server.LOCAL},
	}
	if err := gateway.Run(ctx, cfg); err != nil {
		slog.Error("gateway failed", "error", err)
		os.Exit(1)
	}
}
