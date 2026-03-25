package commander

import (
	"context"

	"github.com/Nesquiko/servermore/pkg/server"
)

type commanderGrpcServer struct {
	UnimplementedCommanderServer
}

var _ CommanderServer = (*commanderGrpcServer)(nil)

func newCommanderGrpcServer(
	ctx context.Context,
	conf CommanderConfig,
	monitoringOpts server.MonitoringOpts,
) (*commanderGrpcServer, func(), error) {
	closer := func() {}
	return &commanderGrpcServer{}, closer, nil
}

// Heartbeat implements [CommanderServer].
func (c *commanderGrpcServer) Heartbeat(
	context.Context,
	*HeartbeatRequest,
) (*HeartbeatResponse, error) {
	return &HeartbeatResponse{}, nil
}

// RegisterRunner implements [CommanderServer].
func (c *commanderGrpcServer) RegisterRunner(
	context.Context,
	*RegisterRunnerRequest,
) (*RegisterRunnerResponse, error) {
	panic("unimplemented")
}

// RouteFunction implements [CommanderServer].
func (c *commanderGrpcServer) RouteFunction(
	context.Context,
	*RouteFunctionRequest,
) (*RouteFunctionResponse, error) {
	panic("unimplemented")
}

type RoutingCache interface {
	FunctionIdInstances(ctx context.Context, funcId string)
}
