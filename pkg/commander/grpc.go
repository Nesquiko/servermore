package commander

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Nesquiko/servermore/pkg/routing"
	"github.com/Nesquiko/servermore/pkg/server"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
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
	ctx context.Context,
	req *RouteFunctionRequest,
) (*RouteFunctionResponse, error) {
	routingData, err := c.commanderService.RouteFunction(ctx, req.GetFunctionId())
	if errors.Is(err, routing.ErrNoRunnerAvailable) {
		return nil, status.Error(codes.Unavailable, err.Error())
	} else if errors.Is(err, ErrFunctionNotFound) {
		return nil, status.Error(codes.NotFound, err.Error())
	} else if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	return &RouteFunctionResponse{
		RunnerAddr: routingData.RunnerAddr,
		InstanceId: routingData.InstanceId,
	}, nil
}
