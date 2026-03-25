package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Nesquiko/servermore/pkg/commander"
)

func main() {
	config := commander.CommanderConfig{
		Env:             "LOCAL",
		AppName:         "commander",
		Host:            "localhost",
		HttpPort:        "42069",
		GrpcPort:        "42070",
		DbURI:           "./tmp/commander.db",
		FuncStorageRoot: "/tmp/commander",
	}

	ctx := context.Background()
	if err := commander.Run(ctx, config); err != nil {
		slog.Error("commander server failed", "error", err)
		os.Exit(1)
	}
}
