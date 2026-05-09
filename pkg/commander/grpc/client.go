package grpc

import (
	"fmt"

	"github.com/Nesquiko/servermore/pkg/server"
	grpc "google.golang.org/grpc"
)

func CreateCommanderClient(
	addr string,
	monitoringOpts server.MonitoringOpts,
) (CommanderClient, *grpc.ClientConn, error) {
	opts := server.MonitoringOpts{
		Env:             monitoringOpts.Env,
		AppName:         fmt.Sprintf("%s-commander-client-%s", monitoringOpts.AppName, addr),
		AppVersion:      monitoringOpts.AppName,
		AdditionalAttrs: monitoringOpts.AdditionalAttrs,
		Level:           monitoringOpts.Level,
		OTELOn:          monitoringOpts.OTELOn,
	}
	conn, err := server.LoggingGrpcClient(addr, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create runner client for address %q: %w", addr, err)
	}

	return NewCommanderClient(conn), conn, nil
}
