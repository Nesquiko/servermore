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
	opts := server.MonitoringOpts{
		AppName:    "gateway",
		AppVersion: "0.0.1",
		Env:        "LOCAL",
	}

	gateway_cfg := gateway.GatewayConfig{}
	if err := gateway.Run(ctx, opts, gateway_cfg); err != nil {
		slog.Error("gateway failed", "error", err)
		os.Exit(1)
	}

}
