package commander

import (
	"context"
	"errors"
	"fmt"
	"time"

	commandergrpc "github.com/Nesquiko/servermore/pkg/commander/grpc"
	"github.com/Nesquiko/servermore/pkg/routing"
	"github.com/Nesquiko/servermore/pkg/server"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

type commanderGrpcServer struct {
	commandergrpc.UnimplementedCommanderServer

	commanderService *CommanderService
}

var _ commandergrpc.CommanderServer = (*commanderGrpcServer)(nil)

func newCommanderGrpcServer(service *CommanderService) (*commanderGrpcServer, func(), error) {
	closer := func() {}
	return &commanderGrpcServer{commanderService: service}, closer, nil
}

// Heartbeat implements [CommanderServer].
func (c *commanderGrpcServer) Heartbeat(
	context.Context,
	*commandergrpc.HeartbeatRequest,
) (*commandergrpc.HeartbeatResponse, error) {
	return &commandergrpc.HeartbeatResponse{}, nil
}

// RegisterRunner implements [CommanderServer].
func (c *commanderGrpcServer) RegisterRunner(
	ctx context.Context,
	req *commandergrpc.RegisterRunnerRequest,
) (*commandergrpc.RegisterRunnerResponse, error) {
	startTime := time.Now()
	meta := &server.RegisterRunnerMeta{RunnerAddr: req.GetAddr()}
	server.SetRegisterRunnerMeta(ctx, meta)

	runn, err := c.commanderService.RegisterRunner(ctx, req.GetAddr())
	if err != nil {
		meta.RegistrationTook = time.Since(startTime)
		return nil, fmt.Errorf("registering runner failed: %w", err)
	}

	meta.RegistrationTook = time.Since(startTime)

	return &commandergrpc.RegisterRunnerResponse{RunnerId: runn.ID}, err
}

// RouteFunction implements [CommanderServer].
func (c *commanderGrpcServer) RouteFunction(
	ctx context.Context,
	req *commandergrpc.RouteFunctionRequest,
) (*commandergrpc.RouteFunctionResponse, error) {
	startTime := time.Now()
	meta := &server.RouteFunctionMeta{FunctionID: req.GetFunctionId()}
	server.SetRouteFunctionMeta(ctx, meta)

	routingData, err := c.commanderService.RouteFunction(ctx, req.GetFunctionId())
	if errors.Is(err, routing.ErrNoRunnerAvailable) {
		meta.RouteTook = time.Since(startTime)
		return nil, status.Error(codes.Unavailable, err.Error())
	} else if errors.Is(err, ErrFunctionNotFound) {
		meta.RouteTook = time.Since(startTime)
		return nil, status.Error(codes.NotFound, err.Error())
	} else if err != nil {
		meta.RouteTook = time.Since(startTime)
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	meta.RunnerAddr = routingData.RunnerAddr
	meta.InstanceID = routingData.InstanceId
	meta.RouteTook = time.Since(startTime)

	return &commandergrpc.RouteFunctionResponse{
		RunnerAddr: routingData.RunnerAddr,
		InstanceId: routingData.InstanceId,
	}, nil
}
