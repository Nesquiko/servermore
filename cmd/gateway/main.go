package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Nesquiko/servermore/pkg/gateway"
	"github.com/Nesquiko/servermore/pkg/server"
)

func main() {
	config := server.ParseFlagsAndLoadConfig[gateway.GatewayConfig]()

	ctx := context.Background()
	if err := gateway.Run(ctx, config); err != nil {
		slog.Error("gateway failed", "error", err)
		os.Exit(1)
	}
}
