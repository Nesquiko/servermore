package commander

import (
	"context"
	"fmt"
	"time"

	"github.com/Nesquiko/servermore/pkg/server"
)

type commanderGrpcServer struct {
	UnimplementedCommanderServer

	commanderService *CommanderService
}

var _ CommanderServer = (*commanderGrpcServer)(nil)

func newCommanderGrpcServer(service *CommanderService) (*commanderGrpcServer, func(), error) {
	closer := func() {}
	return &commanderGrpcServer{commanderService: service}, closer, nil
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
	ctx context.Context,
	req *RegisterRunnerRequest,
) (*RegisterRunnerResponse, error) {
	startTime := time.Now()
	meta := &server.RegisterRunnerMeta{RunnerAddr: req.GetAddr()}
	server.SetRegisterRunnerMeta(ctx, meta)

	runn, err := c.commanderService.RegisterRunner(ctx, req.GetAddr())
	if err != nil {
		meta.RegistrationTook = time.Since(startTime)
		return nil, fmt.Errorf("registering runner failed: %w", err)
	}

	meta.RegistrationTook = time.Since(startTime)

	return &RegisterRunnerResponse{RunnerId: runn.ID}, err
}

// RouteFunction implements [CommanderServer].
func (c *commanderGrpcServer) RouteFunction(
	context.Context,
	*RouteFunctionRequest,
) (*RouteFunctionResponse, error) {
	panic("unimplemented")
}
