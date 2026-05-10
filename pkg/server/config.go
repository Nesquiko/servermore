package server

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/goccy/go-yaml"
)

type Environment string

const (
	LOCAL Environment = "LOCAL"
	TEST  Environment = "TEST"
	PROD  Environment = "PROD"
)

func ParseFlagsAndLoadConfig[CONFIG any]() CONFIG {
	configPath := flag.String("config", "", "path to the YAML config")
	flag.Parse()
	if *configPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	config, err := LoadConfigFromYAML[CONFIG](*configPath)
	if err != nil {
		slog.Error("failed to load config", "config", *configPath, "error", err)
		os.Exit(1)
	}
	return config
}

func LoadConfigFromYAML[C any](configPath string) (C, error) {
	var config C
	if configPath == "" {
		return config, fmt.Errorf("config path is required")
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return config, fmt.Errorf("read config %q: %w", configPath, err)
	}

	if err := yaml.Unmarshal(configBytes, &config); err != nil {
		return config, fmt.Errorf("decode config %q: %w", configPath, err)
	}

	return config, nil
}
