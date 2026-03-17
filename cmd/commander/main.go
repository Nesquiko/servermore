package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Nesquiko/servermore/pkg/commander"
)

var (
	AppName    = "commander"
	CommitHash = "n/a"
	Env        = "LOCAL"
)

func main() {
	config := commander.CommanderHTTPServerConfig{
		AppName:         AppName,
		CommitHash:      CommitHash,
		Env:             Env,
		Host:            "localhost",
		Port:            "42069",
		BaseURL:         "",
		DbURI:           "./tmp/commander.db",
		FuncStorageRoot: "/tmp/commander",
	}

	ctx := context.Background()
	if err := commander.Run(ctx, config); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
