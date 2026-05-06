package main

import (
	"context"

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
	gateway.Run(ctx, opts, gateway_cfg)
}
