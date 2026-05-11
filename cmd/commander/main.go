package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Nesquiko/servermore/pkg/caching"
	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/server"
)

func main() {
	config := server.ParseFlagsAndLoadConfig[commander.CommanderConfig]()

	ctx := context.Background()
	cache := caching.NewInMemoryCache()
	if err := commander.Run(ctx, cache, config); err != nil {
		slog.Error("commander server failed", "error", err)
		os.Exit(1)
	}
}
