package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Nesquiko/servermore/pkg/runner"
	"github.com/Nesquiko/servermore/pkg/server"
)

func main() {
	config := server.ParseFlagsAndLoadConfig[runner.RunnerConfig]()

	if err := runner.Run(context.Background(), config); err != nil {
		slog.Error("runner failed", "error", err)
		os.Exit(1)
	}
}
