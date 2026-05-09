package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/Nesquiko/servermore/pkg/caching"
	"github.com/Nesquiko/servermore/pkg/commander"
	"github.com/Nesquiko/servermore/pkg/server"
)

func main() {
	config := commander.CommanderConfig{
		AppName:                     "commander",
		Env:                         server.LOCAL,
		Host:                        "localhost",
		HttpPort:                    "42069",
		GrpcPort:                    "42070",
		DbURI:                       "./tmp/commander.db",
		FuncStorageRoot:             "/tmp/commander",
		RunnerHeartbeatPoll:         250 * time.Millisecond,
		RunnerOverloadedQueueSize:   256,
		InstanceOverloadedQueueSize: 8,
	}

	ctx := context.Background()
	cache := caching.NewInMemoryCache()
	if err := commander.Run(ctx, cache, config); err != nil {
		slog.Error("commander server failed", "error", err)
		os.Exit(1)
	}
}
